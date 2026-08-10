package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Digest is one project's daily notification content (issue #109's (a)/(b)/(c)):
// overdue open tasks, open tasks due within 24h, and failed sync jobs /
// webhook events. It carries no send/log state of its own — that is the
// Worker's job — so it stays trivial to build and assert on in tests.
type Digest struct {
	ProjectID           uuid.UUID
	Date                time.Time
	Overdue             []db.Task
	DueSoon             []db.Task
	FailedSyncJobs      []db.SyncJob
	FailedWebhookEvents []db.WebhookEvent
}

// Empty reports whether the digest has nothing to report, in which case the
// Worker must not send it at all (issue #109: "対象が無い日は送らない").
func (d Digest) Empty() bool {
	return len(d.Overdue) == 0 && len(d.DueSoon) == 0 && len(d.FailedSyncJobs) == 0 && len(d.FailedWebhookEvents) == 0
}

// BuildDigest gathers projectID's digest content as of today (interpreted
// as a UTC calendar date, matching tasks.due_on which carries no
// time-of-day). It is a pure read: nothing here decides whether or how to
// send, which is what makes it unit-testable against dbtest.FakeQuerier
// without a Sender in the loop.
func BuildDigest(ctx context.Context, q db.Querier, projectID uuid.UUID, today time.Time) (Digest, error) {
	todayDate := pgtype.Date{Time: today, Valid: true}

	overdue, err := q.ListOverdueOpenTasksByProject(ctx, db.ListOverdueOpenTasksByProjectParams{
		ProjectID: projectID,
		Today:     todayDate,
	})
	if err != nil {
		return Digest{}, fmt.Errorf("notification: build digest: overdue tasks: %w", err)
	}
	dueSoon, err := q.ListTasksDueSoonByProject(ctx, db.ListTasksDueSoonByProjectParams{
		ProjectID: projectID,
		Today:     todayDate,
	})
	if err != nil {
		return Digest{}, fmt.Errorf("notification: build digest: due-soon tasks: %w", err)
	}
	failedJobs, err := q.ListFailedSyncJobsByProject(ctx, projectID)
	if err != nil {
		return Digest{}, fmt.Errorf("notification: build digest: failed sync jobs: %w", err)
	}
	failedEvents, err := q.ListFailedWebhookEventsByProject(ctx, projectID)
	if err != nil {
		return Digest{}, fmt.Errorf("notification: build digest: failed webhook events: %w", err)
	}

	return Digest{
		ProjectID:           projectID,
		Date:                today,
		Overdue:             overdue,
		DueSoon:             dueSoon,
		FailedSyncJobs:      failedJobs,
		FailedWebhookEvents: failedEvents,
	}, nil
}
