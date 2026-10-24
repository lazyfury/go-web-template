package message

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WorkerOptions configures the delivery worker.
type WorkerOptions struct {
	PollInterval    time.Duration
	BatchSize       int
	Lease           time.Duration
	RetryBase       time.Duration
	DeliveryTimeout time.Duration
	// Now and Jitter are injectable for deterministic tests.
	Now    func() time.Time
	Jitter func(time.Duration) time.Duration
	Logger *zap.Logger
}

// Worker claims external deliveries and dispatches them through the configured
// channel senders. It never starts itself: cmd/server owns the root goroutine.
type Worker struct {
	repo    Repository
	senders map[Channel]Sender
	opts    WorkerOptions
}

// NewWorker creates a delivery worker. A missing sender makes deliveries for
// that channel dead immediately (there is nothing to retry).
func NewWorker(repo Repository, senders map[Channel]Sender, opts WorkerOptions) *Worker {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Second
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.Lease <= 0 {
		opts.Lease = 2 * time.Minute
	}
	if opts.RetryBase <= 0 {
		opts.RetryBase = 30 * time.Second
	}
	if opts.DeliveryTimeout <= 0 {
		opts.DeliveryTimeout = 30 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Jitter == nil {
		opts.Jitter = defaultJitter
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	copied := make(map[Channel]Sender, len(senders))
	for channel, sender := range senders {
		copied[channel] = sender
	}
	return &Worker{repo: repo, senders: copied, opts: opts}
}

// Run blocks until ctx is canceled. It processes one batch immediately, then
// polls at the configured interval. Transient per-delivery failures are
// recorded in the delivery row; a claim/DB failure is returned to the caller so
// cmd/server can cancel HTTP and exit non-zero.
func (w *Worker) Run(ctx context.Context) error {
	w.opts.Logger.Info("message worker started",
		zap.Duration("poll_interval", w.opts.PollInterval),
		zap.Int("batch_size", w.opts.BatchSize),
	)
	if err := w.RunOnce(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	ticker := time.NewTicker(w.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.opts.Logger.Info("message worker stopped")
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

// RunOnce claims and processes at most one batch. It is exported for tests and
// operational tooling; production uses Run.
func (w *Worker) RunOnce(ctx context.Context) error {
	deliveries, err := w.repo.ClaimDeliveries(ctx, w.opts.Now(), w.opts.BatchSize, w.opts.Lease)
	if err != nil {
		return fmt.Errorf("claim deliveries: %w", err)
	}
	for i := range deliveries {
		if ctx.Err() != nil {
			return nil
		}
		w.process(ctx, &deliveries[i])
	}
	return nil
}

func (w *Worker) process(ctx context.Context, delivery *Delivery) {
	channel := string(delivery.Channel)
	sender, ok := w.senders[delivery.Channel]
	if !ok {
		deliveryAttemptsTotal.WithLabelValues(channel, "no_sender").Inc()
		w.fail(ctx, delivery, "sender not configured", true)
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, w.opts.DeliveryTimeout)
	started := time.Now() // real clock on purpose: this is a measured duration, not a policy input
	providerID, err := sender.Send(sendCtx, delivery)
	deliveryDuration.WithLabelValues(channel).Observe(time.Since(started).Seconds())
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			// Shutdown in progress: leave the lease in place so another
			// instance can retry after it expires.
			return
		}
		dead := delivery.Attempts >= delivery.MaxAttempts
		if dead {
			deliveryAttemptsTotal.WithLabelValues(channel, "dead").Inc()
		} else {
			deliveryAttemptsTotal.WithLabelValues(channel, "failed").Inc()
		}
		w.fail(ctx, delivery, err.Error(), dead)
		if !dead {
			w.opts.Logger.Warn("message delivery failed; scheduled retry",
				zap.Uint64("delivery_id", delivery.ID),
				zap.String("channel", string(delivery.Channel)),
				zap.Int("attempt", delivery.Attempts),
			)
		}
		return
	}
	if err := w.repo.MarkDeliverySent(ctx, delivery.ID, providerID, w.opts.Now()); err != nil {
		w.opts.Logger.Error("mark message delivery sent failed",
			zap.Uint64("delivery_id", delivery.ID),
			zap.Error(err),
		)
		return
	}
	deliveryAttemptsTotal.WithLabelValues(channel, "sent").Inc()
	w.opts.Logger.Info("message delivery sent",
		zap.Uint64("delivery_id", delivery.ID),
		zap.String("channel", string(delivery.Channel)),
		zap.Int("attempt", delivery.Attempts),
	)
}

func (w *Worker) fail(ctx context.Context, delivery *Delivery, reason string, dead bool) {
	nextAttempt := w.opts.Now().Add(w.backoff(delivery.Attempts))
	if err := w.repo.MarkDeliveryFailed(ctx, delivery.ID, truncateError(reason), nextAttempt, dead, w.opts.Now()); err != nil {
		w.opts.Logger.Error("mark message delivery failed",
			zap.Uint64("delivery_id", delivery.ID),
			zap.Error(err),
		)
	}
	if dead {
		w.opts.Logger.Error("message delivery dead",
			zap.Uint64("delivery_id", delivery.ID),
			zap.String("channel", string(delivery.Channel)),
			zap.Int("attempt", delivery.Attempts),
		)
	}
}

func (w *Worker) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := w.opts.RetryBase
	for i := 1; i < attempt; i++ {
		if delay > time.Hour/2 {
			delay = time.Hour
			break
		}
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	if w.opts.Jitter != nil {
		delay = w.opts.Jitter(delay)
	}
	if delay <= 0 {
		delay = time.Second
	}
	return delay
}

func defaultJitter(delay time.Duration) time.Duration {
	// ±20% avoids synchronized retries across instances.
	factor := 0.8 + rand.Float64()*0.4 //nolint:gosec // jitter does not need crypto randomness
	return time.Duration(float64(delay) * factor)
}

func truncateError(message string) string {
	message = strings.TrimSpace(message)
	const maxErrorLength = 500
	if len(message) > maxErrorLength {
		return message[:maxErrorLength]
	}
	return message
}
