package notification_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/notification"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDigest_EmptyWhenNothingToReport(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	digest, err := notification.BuildDigest(context.Background(), q, project.ID, time.Now())
	require.NoError(t, err)

	assert.True(t, digest.Empty(), "a project with no overdue/due-soon tasks and no failures must build an empty digest")
}

func TestBuildDigest_CollectsOverdueDueSoonAndFailedSyncJobs(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")
	today := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	overdue := q.SeedTask(project.ID, owner.ID, "overdue task")
	overdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true}
	q.SetTaskForTest(overdue)

	dueSoon := q.SeedTask(project.ID, owner.ID, "due today")
	dueSoon.DueOn = pgtype.Date{Time: today, Valid: true}
	q.SetTaskForTest(dueSoon)

	dueLater := q.SeedTask(project.ID, owner.ID, "due next week")
	dueLater.DueOn = pgtype.Date{Time: today.AddDate(0, 0, 7), Valid: true}
	q.SetTaskForTest(dueLater)

	closedOverdue := q.SeedTask(project.ID, owner.ID, "closed but overdue")
	closedOverdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -2), Valid: true}
	closedOverdue.Status = "closed"
	q.SetTaskForTest(closedOverdue)

	q.SeedSyncJob(project.ID, "issue.update", "failed", today)
	q.SeedSyncJob(project.ID, "issue.update", "pending", today)

	digest, err := notification.BuildDigest(context.Background(), q, project.ID, today)
	require.NoError(t, err)

	assert.False(t, digest.Empty())
	if assert.Len(t, digest.Overdue, 1) {
		assert.Equal(t, overdue.ID, digest.Overdue[0].ID)
	}
	if assert.Len(t, digest.DueSoon, 1) {
		assert.Equal(t, dueSoon.ID, digest.DueSoon[0].ID)
	}
	assert.Len(t, digest.FailedSyncJobs, 1, "only the failed job counts, not the pending one")
}

func TestBuildDigest_CollectsFailedWebhookEvents(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	conn := q.SeedGitlabConnection(project.ID, []byte("ciphertext"))
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    1,
		PathWithNamespace:  "group/project",
		Name:               "project",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)

	event := q.SeedWebhookEvent(link.ID, []byte(`{}`))
	event.Status = "failed"
	q.SetWebhookEventForTest(event)

	unrelatedProject := q.SeedProject(owner.ID, "other")

	digest, err := notification.BuildDigest(context.Background(), q, project.ID, time.Now())
	require.NoError(t, err)
	assert.Len(t, digest.FailedWebhookEvents, 1)

	otherDigest, err := notification.BuildDigest(context.Background(), q, unrelatedProject.ID, time.Now())
	require.NoError(t, err)
	assert.Empty(t, otherDigest.FailedWebhookEvents, "a failed event under a different project must not leak into this digest")
}
