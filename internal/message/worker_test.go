package message

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/testutil"
)

type workerRepo struct {
	Repository
	claim      []Delivery
	claimErr   error
	renewErr   error
	renews     []leaseRenewal
	events     *[]string
	sent       []uint64
	providerID []string
	failed     []failedDelivery
}

type leaseRenewal struct {
	id       uint64
	previous *time.Time
	until    time.Time
}

func (r *workerRepo) RenewLease(_ context.Context, id uint64, previous *time.Time, lease time.Duration, now time.Time) error {
	r.renews = append(r.renews, leaseRenewal{id: id, previous: previous, until: now.Add(lease)})
	if r.events != nil {
		*r.events = append(*r.events, "renew")
	}
	return r.renewErr
}

type failedDelivery struct {
	id          uint64
	reason      string
	nextAttempt time.Time
	dead        bool
	now         time.Time
}

func (r *workerRepo) ClaimDeliveries(_ context.Context, _ time.Time, _ int, _ time.Duration) ([]Delivery, error) {
	claimed := r.claim
	r.claim = nil
	return claimed, r.claimErr
}

func (r *workerRepo) MarkDeliverySent(_ context.Context, id uint64, providerMessageID string, _ time.Time) error {
	r.sent = append(r.sent, id)
	r.providerID = append(r.providerID, providerMessageID)
	return nil
}

func (r *workerRepo) MarkDeliveryFailed(_ context.Context, id uint64, reason string, nextAttempt time.Time, dead bool, now time.Time) error {
	r.failed = append(r.failed, failedDelivery{id: id, reason: reason, nextAttempt: nextAttempt, dead: dead, now: now})
	return nil
}

type fakeSender struct {
	channel Channel
	id      string
	err     error
	calls   []Delivery
	events  *[]string
}

func (s *fakeSender) Channel() Channel { return s.channel }

func (s *fakeSender) Send(_ context.Context, delivery *Delivery) (string, error) {
	s.calls = append(s.calls, *delivery)
	if s.events != nil {
		*s.events = append(*s.events, "send")
	}
	if s.err != nil {
		return "", s.err
	}
	return s.id, nil
}

func fixedWorker(repo Repository, senders map[Channel]Sender, now time.Time) *Worker {
	return NewWorker(repo, senders, WorkerOptions{
		PollInterval:    time.Hour,
		BatchSize:       10,
		Lease:           time.Minute,
		RetryBase:       10 * time.Second,
		DeliveryTimeout: time.Second,
		Now:             func() time.Time { return now },
		Jitter:          func(d time.Duration) time.Duration { return d },
	})
}

func TestWorkerMarksSent(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	repo := &workerRepo{claim: []Delivery{{
		ID: 1, MessageID: 2, Channel: ChannelEmail, Target: "a@example.com", Payload: `{"subject":"hi","body":"there"}`,
		Attempts: 1, MaxAttempts: 3,
	}}}
	sender := &fakeSender{channel: ChannelEmail, id: "provider-1"}
	worker := fixedWorker(repo, map[Channel]Sender{ChannelEmail: sender}, now)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(sender.calls) != 1 || sender.calls[0].ID != 1 {
		t.Fatalf("sender calls = %+v", sender.calls)
	}
	if len(repo.sent) != 1 || repo.sent[0] != 1 || repo.providerID[0] != "provider-1" {
		t.Fatalf("sent = %+v provider = %+v", repo.sent, repo.providerID)
	}
	if len(repo.failed) != 0 {
		t.Errorf("failed = %+v, want none", repo.failed)
	}
}

func TestWorkerSchedulesRetryAndDead(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	repo := &workerRepo{claim: []Delivery{
		{ID: 1, Channel: ChannelEmail, Attempts: 2, MaxAttempts: 5},
		{ID: 2, Channel: ChannelSMS, Attempts: 5, MaxAttempts: 5},
	}}
	sender := &fakeSender{channel: ChannelEmail, err: errors.New("smtp down")}
	sms := &fakeSender{channel: ChannelSMS, err: errors.New("sms down")}
	worker := fixedWorker(repo, map[Channel]Sender{ChannelEmail: sender, ChannelSMS: sms}, now)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(repo.failed) != 2 {
		t.Fatalf("failed = %+v", repo.failed)
	}
	first := repo.failed[0]
	if first.dead || !first.nextAttempt.Equal(now.Add(20*time.Second)) {
		t.Errorf("first failure = %+v, want retry at +20s", first)
	}
	second := repo.failed[1]
	if !second.dead {
		t.Errorf("second failure = %+v, want dead", second)
	}
}

func TestWorkerMarksMissingSenderDead(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	repo := &workerRepo{claim: []Delivery{{ID: 1, Channel: ChannelWebhook, Attempts: 1, MaxAttempts: 5}}}
	worker := fixedWorker(repo, nil, now)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(repo.failed) != 1 || !repo.failed[0].dead || repo.failed[0].reason != "sender not configured" {
		t.Fatalf("failed = %+v", repo.failed)
	}
}

func TestWorkerRunStopsOnCancel(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	repo := &workerRepo{}
	worker := fixedWorker(repo, nil, now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Errorf("Run canceled = %v, want nil", err)
	}
}

// TestWorkerRecordsDeliveryMetrics 保证投递结果可观测：HTTP 只能看到「发送成功」，
// 邮件失败/进入 dead/没有 sender 只能靠这些计数告警。
func TestWorkerRecordsDeliveryMetrics(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	delta := func(channel, result string, run func()) float64 {
		t.Helper()
		before := testutil.CounterValueFor(t, "message_delivery_attempts_total", "channel", channel, "result", result)
		run()
		return testutil.CounterValueFor(t, "message_delivery_attempts_total", "channel", channel, "result", result) - before
	}

	sent := delta("email", "sent", func() {
		repo := &workerRepo{claim: []Delivery{{ID: 1, Channel: ChannelEmail, Attempts: 1, MaxAttempts: 3}}}
		worker := fixedWorker(repo, map[Channel]Sender{ChannelEmail: &fakeSender{channel: ChannelEmail, id: "p1"}}, now)
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce(sent): %v", err)
		}
	})
	if sent < 1 {
		t.Errorf("email/sent delta = %v, want at least 1", sent)
	}

	failed := delta("sms", "failed", func() {
		repo := &workerRepo{claim: []Delivery{{ID: 2, Channel: ChannelSMS, Attempts: 1, MaxAttempts: 3}}}
		worker := fixedWorker(repo, map[Channel]Sender{ChannelSMS: &fakeSender{channel: ChannelSMS, err: errors.New("sms down")}}, now)
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce(failed): %v", err)
		}
	})
	if failed < 1 {
		t.Errorf("sms/failed delta = %v, want at least 1", failed)
	}

	dead := delta("webhook", "dead", func() {
		repo := &workerRepo{claim: []Delivery{{ID: 3, Channel: ChannelWebhook, Attempts: 2, MaxAttempts: 2}}}
		worker := fixedWorker(repo, map[Channel]Sender{ChannelWebhook: &fakeSender{channel: ChannelWebhook, err: errors.New("403")}}, now)
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce(dead): %v", err)
		}
	})
	if dead < 1 {
		t.Errorf("webhook/dead delta = %v, want at least 1", dead)
	}

	leaseLost := delta("email", "lease_lost", func() {
		repo := &workerRepo{
			claim:    []Delivery{{ID: 9, Channel: ChannelEmail, Attempts: 1, MaxAttempts: 2}},
			renewErr: ErrLeaseLost,
		}
		worker := fixedWorker(repo, map[Channel]Sender{ChannelEmail: &fakeSender{channel: ChannelEmail, id: "p9"}}, now)
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce(lease lost): %v", err)
		}
	})
	if leaseLost < 1 {
		t.Errorf("email/lease_lost delta = %v, want at least 1", leaseLost)
	}

	noSender := delta("webhook", "no_sender", func() {
		repo := &workerRepo{claim: []Delivery{{ID: 4, Channel: ChannelWebhook, Attempts: 1, MaxAttempts: 2}}}
		worker := fixedWorker(repo, nil, now)
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce(no sender): %v", err)
		}
	})
	if noSender < 1 {
		t.Errorf("webhook/no_sender delta = %v, want at least 1", noSender)
	}
}

// TestWorkerRenewsLeaseBeforeSending pins the ordering that prevents duplicate
// deliveries: the row is re-leased immediately before the send, so a batch that
// takes longer than message.lease cannot let another replica claim a row that
// this process is already sending.
func TestWorkerRenewsLeaseBeforeSending(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	var events []string
	repo := &workerRepo{
		claim:  []Delivery{{ID: 7, Channel: ChannelEmail, Attempts: 1, MaxAttempts: 3}},
		events: &events,
	}
	sender := &fakeSender{channel: ChannelEmail, id: "provider-7", events: &events}
	worker := fixedWorker(repo, map[Channel]Sender{ChannelEmail: sender}, now)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(repo.renews) != 1 || repo.renews[0].id != 7 {
		t.Fatalf("renews = %+v, want one renewal for delivery 7", repo.renews)
	}
	if !repo.renews[0].until.Equal(now.Add(time.Minute)) {
		t.Errorf("renewed until %v, want %v", repo.renews[0].until, now.Add(time.Minute))
	}
	if len(events) != 2 || events[0] != "renew" || events[1] != "send" {
		t.Errorf("events = %v, want renew before send", events)
	}
}

// TestWorkerSkipsSendAfterLosingLease covers the reclaim race: the row was
// taken over (or finished) by someone else, so this worker must not send, must
// not mark it sent and must not record a failure against the new owner.
func TestWorkerSkipsSendAfterLosingLease(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	repo := &workerRepo{
		claim:    []Delivery{{ID: 8, Channel: ChannelSMS, Attempts: 1, MaxAttempts: 3}},
		renewErr: ErrLeaseLost,
	}
	sender := &fakeSender{channel: ChannelSMS, id: "provider-8"}
	worker := fixedWorker(repo, map[Channel]Sender{ChannelSMS: sender}, now)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Errorf("sender calls = %+v, want none", sender.calls)
	}
	if len(repo.sent) != 0 || len(repo.failed) != 0 {
		t.Errorf("sent = %+v failed = %+v, want no state change", repo.sent, repo.failed)
	}
}
