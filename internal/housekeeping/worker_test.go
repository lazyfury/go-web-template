package housekeeping

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/testutil"
)

func TestRunOncePassesCutoffPerTask(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var (
		mu      sync.Mutex
		cutoffs = map[string]time.Time{}
	)
	record := func(name string, retention time.Duration) Task {
		return Task{
			Name:      name,
			Retention: retention,
			Purge: func(_ context.Context, before time.Time) (int64, error) {
				mu.Lock()
				cutoffs[name] = before
				mu.Unlock()
				return 0, nil
			},
		}
	}

	worker := NewWorker([]Task{
		record("tokens", 7*24*time.Hour),
		record("messages", 90*24*time.Hour),
	}, Options{Now: func() time.Time { return now }})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got, want := cutoffs["tokens"], now.Add(-7*24*time.Hour); !got.Equal(want) {
		t.Errorf("tokens cutoff = %v, want %v", got, want)
	}
	if got, want := cutoffs["messages"], now.Add(-90*24*time.Hour); !got.Equal(want) {
		t.Errorf("messages cutoff = %v, want %v", got, want)
	}
}

func TestRunOnceKeepsGoingAfterAFailedTask(t *testing.T) {
	t.Parallel()

	var ran []string
	worker := NewWorker([]Task{
		{
			Name: "broken",
			Purge: func(context.Context, time.Time) (int64, error) {
				return 0, errors.New("relation does not exist")
			},
		},
		{
			Name: "healthy",
			Purge: func(context.Context, time.Time) (int64, error) {
				ran = append(ran, "healthy")
				return 3, nil
			},
		},
	}, Options{})

	err := worker.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce returned nil, want the broken task's error")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q does not name the failing task", err)
	}
	if len(ran) != 1 {
		t.Errorf("healthy task ran %d times, want 1 (one failure must not block the rest)", len(ran))
	}
}

func TestRunOnceBoundsEachTaskWithATimeout(t *testing.T) {
	t.Parallel()

	var sawDeadline bool
	worker := NewWorker([]Task{{
		Name: "slow",
		Purge: func(ctx context.Context, _ time.Time) (int64, error) {
			if _, ok := ctx.Deadline(); ok {
				sawDeadline = true
			}
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}}, Options{Timeout: 10 * time.Millisecond})

	start := time.Now()
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce returned nil, want a timeout error")
	}
	if !sawDeadline {
		t.Error("task context carried no deadline")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("RunOnce took %v, want it bounded by the task timeout", elapsed)
	}
}

// TestRunStopsOnCancel 钉住「ctx 取消后停止领取新任务并等待在途任务」。
func TestRunStopsOnCancel(t *testing.T) {
	t.Parallel()

	var (
		entered  = make(chan struct{})
		finished atomic.Bool
		once     sync.Once
	)
	worker := NewWorker([]Task{{
		Name: "inflight",
		Purge: func(ctx context.Context, _ time.Time) (int64, error) {
			once.Do(func() { close(entered) })
			<-ctx.Done()
			finished.Store(true)
			return 0, ctx.Err()
		},
	}}, Options{Interval: time.Millisecond, Timeout: time.Minute})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("purge task never started")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if !finished.Load() {
		t.Error("Run returned before the in-flight purge finished")
	}
}

func TestRunWithoutTasksReturnsImmediately(t *testing.T) {
	t.Parallel()

	if got := NewWorker(nil, Options{}).Tasks(); got != 0 {
		t.Fatalf("Tasks = %d, want 0", got)
	}
	done := make(chan error, 1)
	go func() { done <- NewWorker(nil, Options{}).Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run blocked with no tasks")
	}
}

// TestRunSurfacesFailures 钉住失败可见：清理任务出错必须返回给 cmd/server（取消
// HTTP + 非零退出），而不是只打一行日志让表继续膨胀。
func TestRunSurfacesFailures(t *testing.T) {
	t.Parallel()

	worker := NewWorker([]Task{{
		Name: "broken",
		Purge: func(context.Context, time.Time) (int64, error) {
			return 0, errors.New("permission denied")
		},
	}}, Options{Interval: time.Hour})

	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil, want the purge error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run swallowed the failure instead of returning it")
	}
}

// TestRunOnceRecordsRetentionMetrics 钉住「清理量」与「失败」都可观测：一个静默
// 失败的 janitor 只会在磁盘满的时候才被发现。
func TestRunOnceRecordsRetentionMetrics(t *testing.T) {
	t.Parallel()

	purged := testutil.CounterValueFor(t, "housekeeping_purged_rows_total", "task", "metric-ok")
	failures := testutil.CounterValueFor(t, "housekeeping_task_failures_total", "task", "metric-broken")

	worker := NewWorker([]Task{
		{
			Name: "metric-ok",
			Purge: func(context.Context, time.Time) (int64, error) {
				return 7, nil
			},
		},
		{
			Name: "metric-broken",
			Purge: func(context.Context, time.Time) (int64, error) {
				return 0, errors.New("lock timeout")
			},
		},
	}, Options{})

	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce returned nil, want the broken task's error")
	}

	if got := testutil.CounterValueFor(t, "housekeeping_purged_rows_total", "task", "metric-ok") - purged; got != 7 {
		t.Errorf("purged rows delta = %v, want 7", got)
	}
	if got := testutil.CounterValueFor(t, "housekeeping_task_failures_total", "task", "metric-broken") - failures; got < 1 {
		t.Errorf("task failure delta = %v, want at least 1", got)
	}
}
