//go:build integration

// ApplyWebhookTaskFields' closed_at rule against a live PostgreSQL.
//
// The rule is a CASE/COALESCE inside one UPDATE statement, and dbtest's
// FakeQuerier hand-reimplements it — so the unit tests in
// internal/projectsync and internal/webhookapply prove those packages pass
// the right arguments, not that the SQL does the right thing with them. The
// ordering inside the COALESCE is the entire fix (GitLab's value first, so a
// resync repairs an already-imported task rather than preserving the
// import-time timestamp forever), and this is where that stays proven.
//
// Run with: make test-integration
package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyClosed is one ApplyWebhookTaskFields call, varying only the two
// fields this rule reads.
func applyClosed(t *testing.T, q *db.Queries, taskID uuid.UUID, status string, gitlabClosedAt *time.Time) db.Task {
	t.Helper()

	closedAt := pgtype.Timestamptz{}
	if gitlabClosedAt != nil {
		closedAt = pgtype.Timestamptz{Time: *gitlabClosedAt, Valid: true}
	}
	got, err := q.ApplyWebhookTaskFields(context.Background(), db.ApplyWebhookTaskFieldsParams{
		ID:             taskID,
		Title:          "Task",
		Description:    "",
		Labels:         []string{},
		Status:         status,
		GitlabClosedAt: closedAt,
	})
	require.NoError(t, err)
	return got
}

func TestApplyWebhookTaskFieldsClosedAtPrefersGitlabsValue(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	owner := createUser(t, q, "closedat")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})
	newTask := func(t *testing.T) db.Task {
		t.Helper()
		tsk, err := q.CreateTask(ctx, db.CreateTaskParams{
			ProjectID: p.ID, Title: "Task", Labels: []string{},
			Priority: "medium", Progress: "not_started", Size: "m", CreatedByUserID: owner.ID,
		})
		require.NoError(t, err)
		return tsk
	}

	longAgo := time.Now().AddDate(0, 0, -90).Truncate(time.Second)
	older := time.Now().AddDate(0, 0, -120).Truncate(time.Second)

	t.Run("a supplied value is written as-is", func(t *testing.T) {
		tsk := newTask(t)
		got := applyClosed(t, q, tsk.ID, "closed", &longAgo)
		require.True(t, got.ClosedAt.Valid)
		assert.WithinDuration(t, longAgo, got.ClosedAt.Time, time.Second)
	})

	t.Run("no supplied value falls back to now", func(t *testing.T) {
		tsk := newTask(t)
		got := applyClosed(t, q, tsk.ID, "closed", nil)
		require.True(t, got.ClosedAt.Valid)
		assert.WithinDuration(t, time.Now(), got.ClosedAt.Time, time.Minute)
	})

	// The repair path: a row already carrying an import-time timestamp is
	// overwritten by GitLab's own. This is the assertion that would fail if
	// the COALESCE were written COALESCE(closed_at, $9, now()).
	t.Run("a supplied value overwrites an existing one", func(t *testing.T) {
		tsk := newTask(t)
		applyClosed(t, q, tsk.ID, "closed", nil) // the pre-fix state
		got := applyClosed(t, q, tsk.ID, "closed", &older)
		assert.WithinDuration(t, older, got.ClosedAt.Time, time.Second)
	})

	// ...but re-applying without one never moves it forward, so a GitLab
	// that reports no closed_at cannot drag every resynced task's completion
	// time to the present.
	t.Run("no supplied value preserves an existing one", func(t *testing.T) {
		tsk := newTask(t)
		applyClosed(t, q, tsk.ID, "closed", &longAgo)
		got := applyClosed(t, q, tsk.ID, "closed", nil)
		assert.WithinDuration(t, longAgo, got.ClosedAt.Time, time.Second)
	})

	t.Run("reopening clears it whatever is supplied", func(t *testing.T) {
		tsk := newTask(t)
		applyClosed(t, q, tsk.ID, "closed", &longAgo)
		got := applyClosed(t, q, tsk.ID, "open", &longAgo)
		assert.False(t, got.ClosedAt.Valid)
	})
}
