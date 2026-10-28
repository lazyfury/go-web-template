// Package housekeeping deletes rows that outlived their retention window.
//
// Why a separate package: retention is an operational policy, not a business
// rule. The repositories own the DELETE statements (they know their schema),
// the composition root decides which tables get pruned and for how long, and
// this worker only schedules them. Nothing here knows about auth or messages,
// so the same scheduler can carry any future domain's retention task.
//
// Lifecycle: like every worker in this repository, the constructor is inert.
// cmd/server calls Run(ctx) from its errgroup (ADR 14); App only builds it.
package housekeeping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/workerx"
)

var tracer = otel.Tracer("github.com/example/go-web-template/housekeeping")

// Task is one retention rule.
type Task struct {
	// Name identifies the task in logs and metrics ("refresh_tokens", ...).
	Name string
	// Retention is how long a row is kept after the moment it becomes
	// prunable. The worker passes now-Retention to Purge as the cutoff.
	Retention time.Duration
	// Purge hard-deletes rows older than the cutoff and reports how many went
	// away. It must honor ctx: the worker enforces a per-task timeout.
	Purge func(ctx context.Context, before time.Time) (int64, error)
}

// Options tunes the scheduler.
type Options struct {
	// Interval is how often the whole task list runs.
	Interval time.Duration
	// Timeout bounds one task, so a slow DELETE cannot hold the tick forever.
	Timeout time.Duration
	// Now is injectable for deterministic tests.
	Now    func() time.Time
	Logger *zap.Logger
}

// Worker schedules retention tasks. It never starts itself.
type Worker struct {
	tasks []Task
	opts  Options
}

// NewWorker creates a retention worker. A nil/empty task list makes Run a no-op
// so the composition root can wire it unconditionally when retention is enabled.
func NewWorker(tasks []Task, opts Options) *Worker {
	if opts.Interval <= 0 {
		opts.Interval = time.Hour
	}
	if opts.Timeout <= 0 {
		opts.Timeout = time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	copied := make([]Task, len(tasks))
	copy(copied, tasks)
	return &Worker{tasks: copied, opts: opts}
}

// Tasks reports how many retention rules are registered.
func (w *Worker) Tasks() int { return len(w.tasks) }

// Run blocks until ctx is canceled. It runs one pass immediately (so a restart
// makes progress instead of waiting a full interval), then every Interval.
//
// A failed task is returned to the caller instead of being swallowed: silently
// ignoring a broken retention job means a table grows until the disk does.
// cmd/server turns that into a canceled HTTP server and a non-zero exit.
func (w *Worker) Run(ctx context.Context) error {
	if len(w.tasks) == 0 {
		return nil
	}
	w.opts.Logger.Info("housekeeping worker started",
		zap.Duration("interval", w.opts.Interval),
		zap.Int("tasks", len(w.tasks)),
	)
	if err := w.RunOnce(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	ticker := time.NewTicker(w.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.opts.Logger.Info("housekeeping worker stopped")
			return nil
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// RunOnce runs every task once. Independent tasks do not share a failure: one
// broken table must not stop the others from being pruned. Exported for tests
// and operational tooling; production uses Run.
func (w *Worker) RunOnce(ctx context.Context) error {
	var errs []error
	for _, task := range w.tasks {
		if ctx.Err() != nil {
			break
		}
		if err := w.runTask(ctx, task); err != nil {
			if ctx.Err() != nil {
				break
			}
			errs = append(errs, err)
		}
	}
	joined := errors.Join(errs...)
	if joined != nil {
		return joined
	}
	// Only a pass where every task succeeded counts as healthy; a failing table
	// would otherwise keep the timestamp fresh while growing unbounded.
	workerx.MarkSuccess(workerx.Housekeeping, w.opts.Now())
	return nil
}

func (w *Worker) runTask(ctx context.Context, task Task) error {
	ctx, span := tracer.Start(ctx, "housekeeping.task",
		oteltrace.WithAttributes(attribute.String("housekeeping.task", task.Name)))
	defer span.End()

	before := w.opts.Now().Add(-task.Retention)
	taskCtx, cancel := context.WithTimeout(ctx, w.opts.Timeout)
	defer cancel()

	deleted, err := task.Purge(taskCtx, before)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "purge failed")
		taskFailuresTotal.WithLabelValues(task.Name).Inc()
		w.opts.Logger.Error("housekeeping task failed",
			zap.String("task", task.Name),
			zap.Error(err),
		)
		return fmt.Errorf("housekeeping task %q: %w", task.Name, err)
	}
	if deleted > 0 {
		purgedRowsTotal.WithLabelValues(task.Name).Add(float64(deleted))
		w.opts.Logger.Info("housekeeping task purged rows",
			zap.String("task", task.Name),
			zap.Int64("rows", deleted),
			zap.Time("before", before),
		)
	} else {
		w.opts.Logger.Debug("housekeeping task found nothing to purge",
			zap.String("task", task.Name),
			zap.Time("before", before),
		)
	}
	return nil
}
