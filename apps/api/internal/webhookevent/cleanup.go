package webhookevent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Retention policy decided for issue #26: only 'processed' events are
// pruned, and only once older than DefaultRetentionPeriod. 'failed' and
// 'skipped' rows are kept indefinitely — their error and skip_reason are
// the whole point of troubleshooting — and 'pending' rows are never touched
// by cleanup; internal/webhookapply's worker is the only thing that moves a
// pending row out of that state.
const DefaultRetentionPeriod = 30 * 24 * time.Hour

// DefaultCleanupInterval is how often a CleanupWorker runs its sweep.
const DefaultCleanupInterval = time.Hour

// CleanupProcessed deletes every 'processed' event whose processed_at is
// older than retention and returns how many rows were removed.
func (s *Service) CleanupProcessed(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)
	deleted, err := s.q.DeleteProcessedWebhookEventsOlderThan(ctx, toTimestamptz(cutoff))
	if err != nil {
		return 0, fmt.Errorf("webhookevent: cleanup processed: %w", err)
	}
	return deleted, nil
}

// CleanupWorker periodically sweeps old 'processed' events. It mirrors
// internal/webhookapply.Worker's Run/Stop shape, adapted to a fixed-interval
// sweep rather than draining a queue: there is no backlog to drain, just a
// housekeeping pass that either finds old rows to delete or doesn't.
type CleanupWorker struct {
	service   *Service
	retention time.Duration
	interval  time.Duration

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

// CleanupOption configures a CleanupWorker at construction time.
type CleanupOption func(*CleanupWorker)

// WithRetention overrides DefaultRetentionPeriod.
func WithRetention(d time.Duration) CleanupOption {
	return func(w *CleanupWorker) { w.retention = d }
}

// WithCleanupInterval overrides DefaultCleanupInterval.
func WithCleanupInterval(d time.Duration) CleanupOption {
	return func(w *CleanupWorker) { w.interval = d }
}

// NewCleanupWorker constructs a CleanupWorker over service.
func NewCleanupWorker(service *Service, opts ...CleanupOption) *CleanupWorker {
	w := &CleanupWorker{
		service:   service,
		retention: DefaultRetentionPeriod,
		interval:  DefaultCleanupInterval,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Run sweeps immediately, then again every interval, until Stop is called or
// ctx is done.
func (w *CleanupWorker) Run(ctx context.Context) error {
	defer close(w.done)

	for {
		if deleted, err := w.service.CleanupProcessed(ctx, w.retention); err != nil {
			slog.ErrorContext(ctx, "webhook event cleanup: sweep failed", "error", err)
		} else if deleted > 0 {
			slog.InfoContext(ctx, "webhook event cleanup: swept processed events", "deleted", deleted)
		}

		select {
		case <-w.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.interval):
		}
	}
}

// Stop tells Run to stop after its current sweep, then blocks until Run
// returns or ctx is done, whichever comes first. Safe to call more than once.
func (w *CleanupWorker) Stop(ctx context.Context) error {
	w.stopOnce.Do(func() { close(w.stop) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
