package message

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerRepo struct {
	Repository
	claim      []Delivery
	claimErr   error
	sent       []uint64
	providerID []string
	failed     []failedDelivery
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
}

func (s *fakeSender) Channel() Channel { return s.channel }

func (s *fakeSender) Send(_ context.Context, delivery *Delivery) (string, error) {
	s.calls = append(s.calls, *delivery)
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
