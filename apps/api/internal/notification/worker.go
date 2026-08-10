package notification

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DefaultSweepInterval mirrors the other background workers' polling
// cadence (sync.Worker, webhookevent.CleanupWorker): frequent enough that a
// project's send_hour is never missed by more than an interval, without
// hammering the database.
const DefaultSweepInterval = 15 * time.Minute

// Worker periodically checks every project with notifications enabled and
// sends its daily digest once its configured send_hour (UTC) has been
// reached, at most once per calendar day per project.
type Worker struct {
	q      db.Querier
	sender Sender
	now    func() time.Time

	interval time.Duration

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

// WorkerOption configures a Worker at construction time.
type WorkerOption func(*Worker)

// WithSweepInterval overrides DefaultSweepInterval.
func WithSweepInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.interval = d }
}

// withNow overrides the Worker's clock; only used by tests.
func withNow(now func() time.Time) WorkerOption {
	return func(w *Worker) { w.now = now }
}

// NewWorker constructs a Worker. q is typically database.NewQuerier(pool):
// like sync.Worker, it is never scoped to a caller's transaction.
func NewWorker(q db.Querier, sender Sender, opts ...WorkerOption) *Worker {
	w := &Worker{
		q:        q,
		sender:   sender,
		now:      time.Now,
		interval: DefaultSweepInterval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Sweep checks every enabled project and sends the digest for each one
// whose send_hour (UTC) has been reached and that has not already sent (or
// attempted to send) a digest today. It is exported so tests can drive the
// worker deterministically instead of waiting on Run's ticker.
func (w *Worker) Sweep(ctx context.Context) {
	settings, err := w.q.ListEnabledNotificationSettings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "notification worker: list enabled settings failed", "error", err)
		return
	}

	nowUTC := w.now().UTC()
	for _, s := range settings {
		if nowUTC.Hour() < int(s.SendHour) {
			continue
		}
		w.sweepProject(ctx, s, nowUTC)
	}
}

func (w *Worker) sweepProject(ctx context.Context, s db.NotificationSetting, nowUTC time.Time) {
	digest, err := BuildDigest(ctx, w.q, s.ProjectID, nowUTC)
	if err != nil {
		slog.ErrorContext(ctx, "notification worker: build digest failed", "project_id", s.ProjectID, "error", err)
		return
	}
	if digest.Empty() {
		return
	}

	today := pgtype.Date{Time: truncateToDate(nowUTC), Valid: true}
	logRow, err := w.q.InsertNotificationDigestLog(ctx, db.InsertNotificationDigestLogParams{
		ProjectID:  s.ProjectID,
		DigestDate: today,
		Status:     "sent",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Another sweep (this tick or a previous one) already sent or
			// attempted today's digest for this project.
			return
		}
		slog.ErrorContext(ctx, "notification worker: insert digest log failed", "project_id", s.ProjectID, "error", err)
		return
	}

	if err := w.sender.Send(ctx, s.WebhookUrl, digest); err != nil {
		slog.ErrorContext(ctx, "notification worker: send digest failed", "project_id", s.ProjectID, "error", err)
		if markErr := w.q.MarkNotificationDigestFailed(ctx, db.MarkNotificationDigestFailedParams{
			ID:    logRow.ID,
			Error: err.Error(),
		}); markErr != nil {
			slog.ErrorContext(ctx, "notification worker: mark digest failed failed", "project_id", s.ProjectID, "error", markErr)
		}
	}
}

func truncateToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Run sweeps immediately, then again every interval, until Stop is called or
// ctx is done. It mirrors webhookevent.CleanupWorker.Run's shape.
func (w *Worker) Run(ctx context.Context) error {
	defer close(w.done)

	for {
		w.Sweep(ctx)

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
func (w *Worker) Stop(ctx context.Context) error {
	w.stopOnce.Do(func() { close(w.stop) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
