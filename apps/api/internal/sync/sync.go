// Package sync implements the outbox worker described in
// docs/plans/issue-sync.md ("Sync engine"): handlers write a task and enqueue
// a sync_jobs row in the same transaction, so an accepted request is always
// eventually pushed, and a background Worker claims, executes and retries
// those jobs. The job handlers themselves (the actual GitLab calls) are
// registered from outside this package by kind; none exist yet.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Handler executes one sync job's side effect (e.g. a GitLab API call). It is
// looked up by the job's kind; a job whose kind has no registered Handler is
// treated as a failed attempt like any other error.
type Handler func(ctx context.Context, job db.SyncJob) error

// Registry maps a sync_jobs.kind to the Handler that executes it. The zero
// value is not usable; construct one with NewRegistry.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}}
}

// Register binds kind to h, overwriting any previous handler for kind.
func (r *Registry) Register(kind string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = h
}

func (r *Registry) lookup(kind string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[kind]
	return h, ok
}

// EnqueueParams holds the fields accepted when enqueueing a sync job.
type EnqueueParams struct {
	ProjectID uuid.UUID
	TaskID    *uuid.UUID // nil for jobs not tied to one task (e.g. project.import)
	Kind      string
	Payload   []byte // stored as-is; nil is normalized to an empty JSON object
	DedupeKey string // empty: no dedupe, always creates a new job
}

// Enqueue inserts params as a pending sync job. q should be scoped to the
// caller's transaction (e.g. db.New(tx)) when the enqueue must be atomic with
// the write that triggered it — that is what lets a handler write a task and
// enqueue its sync job in one commit, per docs/plans/issue-sync.md.
//
// When DedupeKey collides with a job that is still pending, that job's
// payload is refreshed and reused instead of creating a duplicate, collapsing
// rapid repeated edits (e.g. several title changes in a row) into one pending
// job. A collision with a job that already reached running, succeeded or
// failed is not reused: those states clear their own dedupe_key on
// completion (see MarkSyncJobSucceeded / MarkSyncJobFailed), so this is the
// rarer case of enqueueing while the previous attempt is still in flight —
// Enqueue reads that job back as-is rather than erroring.
func Enqueue(ctx context.Context, q db.Querier, params EnqueueParams) (db.SyncJob, error) {
	dedupeKey := pgtype.Text{}
	if params.DedupeKey != "" {
		dedupeKey = pgtype.Text{String: params.DedupeKey, Valid: true}
	}
	taskID := pgtype.UUID{}
	if params.TaskID != nil {
		taskID = pgtype.UUID{Bytes: *params.TaskID, Valid: true}
	}
	payload := params.Payload
	if payload == nil {
		payload = []byte("{}")
	}

	job, err := q.EnqueueSyncJob(ctx, db.EnqueueSyncJobParams{
		ProjectID: params.ProjectID,
		TaskID:    taskID,
		Kind:      params.Kind,
		Payload:   payload,
		DedupeKey: dedupeKey,
	})
	if err == nil {
		return job, nil
	}
	if !dedupeKey.Valid || !errors.Is(err, pgx.ErrNoRows) {
		return db.SyncJob{}, fmt.Errorf("sync: enqueue: %w", err)
	}

	existing, getErr := q.GetSyncJobByDedupeKey(ctx, dedupeKey)
	if getErr != nil {
		return db.SyncJob{}, fmt.Errorf("sync: enqueue: %w", getErr)
	}
	return existing, nil
}

// Default tuning for a Worker; every one is overridable via an Option.
const (
	DefaultBatchSize    = 10
	DefaultMaxAttempts  = 5
	DefaultBaseBackoff  = time.Second
	DefaultStaleAfter   = 5 * time.Minute
	DefaultPollInterval = 5 * time.Second
)

// Worker claims pending sync_jobs rows and executes them through a Registry,
// retrying with exponential backoff and giving up (status 'failed') after
// MaxAttempts. The zero value is not usable; construct one with NewWorker.
type Worker struct {
	q        db.Querier
	registry *Registry

	pollInterval time.Duration
	batchSize    int32
	maxAttempts  int32
	baseBackoff  time.Duration
	staleAfter   time.Duration

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

// Option configures a Worker at construction time.
type Option func(*Worker)

// WithPollInterval sets how long Run sleeps between polls when the previous
// poll claimed no jobs. Default: DefaultPollInterval.
func WithPollInterval(d time.Duration) Option { return func(w *Worker) { w.pollInterval = d } }

// WithBatchSize sets how many jobs Poll claims per call. Default: DefaultBatchSize.
func WithBatchSize(n int32) Option { return func(w *Worker) { w.batchSize = n } }

// WithMaxAttempts sets how many attempts a job gets before it is marked
// 'failed'. Default: DefaultMaxAttempts.
func WithMaxAttempts(n int32) Option { return func(w *Worker) { w.maxAttempts = n } }

// WithBaseBackoff sets the unit exponential backoff is computed from:
// attempt N waits base * 2^(N-1). Default: DefaultBaseBackoff.
func WithBaseBackoff(d time.Duration) Option { return func(w *Worker) { w.baseBackoff = d } }

// WithStaleAfter sets how old a 'running' job's updated_at must be before
// ReclaimStale returns it to 'pending'. Default: DefaultStaleAfter.
func WithStaleAfter(d time.Duration) Option { return func(w *Worker) { w.staleAfter = d } }

// NewWorker constructs a Worker. q is typically database.NewQuerier(pool):
// unlike Enqueue, a Worker is never scoped to a caller's transaction.
func NewWorker(q db.Querier, registry *Registry, opts ...Option) *Worker {
	w := &Worker{
		q:            q,
		registry:     registry,
		pollInterval: DefaultPollInterval,
		batchSize:    DefaultBatchSize,
		maxAttempts:  DefaultMaxAttempts,
		baseBackoff:  DefaultBaseBackoff,
		staleAfter:   DefaultStaleAfter,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// ReclaimStale returns every 'running' job whose updated_at is older than
// StaleAfter to 'pending'. Call it once at startup, before Run's poll loop
// starts, to requeue jobs a previous process claimed and then crashed on
// mid-execution — a job's row otherwise stays 'running' forever, since only
// the worker that claimed it would normally move it out of that state.
func (w *Worker) ReclaimStale(ctx context.Context) (int64, error) {
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-w.staleAfter), Valid: true}
	n, err := w.q.ReclaimStaleRunningSyncJobs(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("sync: reclaim stale jobs: %w", err)
	}
	return n, nil
}

// Poll claims up to BatchSize due pending jobs and executes each in turn,
// returning how many it claimed. It is exported so tests can drive the
// worker deterministically instead of waiting on the poll-interval ticker;
// Run calls it in a loop.
func (w *Worker) Poll(ctx context.Context) (int, error) {
	jobs, err := w.q.ClaimPendingSyncJobs(ctx, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("sync: claim jobs: %w", err)
	}
	for _, job := range jobs {
		w.execute(ctx, job)
	}
	return len(jobs), nil
}

// Run reclaims stale jobs, then polls for pending jobs — looping immediately
// after a poll that claimed a full or partial batch, and sleeping
// PollInterval after one that claimed nothing — until Stop is called or ctx
// is done.
//
// ctx also bounds every claim/handler/status-update call Run makes, so it
// should normally be a long-lived context (e.g. context.Background()), not
// one tied to shutdown: cancelling it aborts whatever job is currently
// running rather than letting it finish. Stop is the graceful-shutdown
// signal — it stops Run from claiming new jobs but lets an in-flight one
// complete.
func (w *Worker) Run(ctx context.Context) error {
	defer close(w.done)

	if _, err := w.ReclaimStale(ctx); err != nil {
		return err
	}

	for {
		select {
		case <-w.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := w.Poll(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "sync worker: poll failed", "error", err)
		}
		if n > 0 {
			continue
		}

		select {
		case <-w.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}
}

// Stop tells Run to stop claiming new jobs, then blocks until Run returns
// (i.e. until any job it is currently executing finishes) or ctx is done,
// whichever comes first. It is safe to call more than once; later calls
// return once the first Stop's wait is satisfied.
func (w *Worker) Stop(ctx context.Context) error {
	w.stopOnce.Do(func() { close(w.stop) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// execute runs job's handler and records the outcome: success clears it to
// 'succeeded', failure retries with backoff until MaxAttempts, then gives up
// and marks it 'failed'. Status-update errors are logged, not returned —
// they must never abort the poll loop, or one bad row would wedge the whole
// worker.
func (w *Worker) execute(ctx context.Context, job db.SyncJob) {
	handler, ok := w.registry.lookup(job.Kind)
	var err error
	if !ok {
		err = fmt.Errorf("sync: no handler registered for kind %q", job.Kind)
	} else {
		err = handler(ctx, job)
	}

	if err == nil {
		if markErr := w.q.MarkSyncJobSucceeded(ctx, job.ID); markErr != nil {
			slog.ErrorContext(ctx, "sync worker: mark succeeded failed", "job_id", job.ID, "error", markErr)
		}
		return
	}

	attempts := job.Attempts + 1
	if attempts >= w.maxAttempts {
		if markErr := w.q.MarkSyncJobFailed(ctx, db.MarkSyncJobFailedParams{
			ID:        job.ID,
			LastError: err.Error(),
		}); markErr != nil {
			slog.ErrorContext(ctx, "sync worker: mark failed failed", "job_id", job.ID, "error", markErr)
		}
		return
	}

	runAfter := time.Now().Add(backoff(attempts, w.baseBackoff))
	if markErr := w.q.MarkSyncJobRetry(ctx, db.MarkSyncJobRetryParams{
		ID:        job.ID,
		RunAfter:  pgtype.Timestamptz{Time: runAfter, Valid: true},
		LastError: err.Error(),
	}); markErr != nil {
		slog.ErrorContext(ctx, "sync worker: mark retry failed", "job_id", job.ID, "error", markErr)
	}
}

// backoff returns base * 2^(attempts-1): base after the 1st failed attempt,
// 2*base after the 2nd, 4*base after the 3rd, and so on.
func backoff(attempts int32, base time.Duration) time.Duration {
	d := base
	for i := int32(1); i < attempts; i++ {
		d *= 2
	}
	return d
}
