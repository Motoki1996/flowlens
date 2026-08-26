//go:build integration

package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two-axis assignee filter is the one piece of this feature the fake
// querier can only approximate, so it gets a real-SQL test: a task assigned in
// FlowLens only, one assigned on GitLab only (as a synced issue would be), and
// one assigned to nobody must all sort into the right buckets for the same
// person.
func TestListTasksByProject_AssigneeFilterMatchesBothAxes(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	owner := createUser(t, q, "assignee-owner")
	member := createUser(t, q, "assignee-member")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{
		OwnerUserID: owner.ID,
		Name:        fmt.Sprintf("Assignee-%d", time.Now().UnixNano()),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: member.ID, Role: "member"})
	require.NoError(t, err)

	baseURL := "https://gitlab.example.com"
	_, err = q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID: p.ID, BaseUrl: baseURL, EncryptedToken: []byte("ciphertext"),
	})
	require.NoError(t, err)
	_, err = q.UpsertUserGitlabIdentity(ctx, db.UpsertUserGitlabIdentityParams{
		UserID: member.ID, GitlabBaseUrl: baseURL, GitlabUserID: 4242, GitlabUsername: "member-gl",
	})
	require.NoError(t, err)

	newTask := func(title string, assigneeUserID pgtype.UUID, gitlabID pgtype.Int8) db.Task {
		row, err := q.CreateTask(ctx, db.CreateTaskParams{
			ProjectID: p.ID, Title: title, Labels: []string{},
			Priority: "medium", Progress: "not_started", Size: "m",
			CreatedByUserID:      owner.ID,
			AssigneeUserID:       assigneeUserID,
			AssigneeGitlabUserID: gitlabID,
		})
		require.NoError(t, err)
		return row
	}
	memberUUID := pgtype.UUID{Bytes: member.ID, Valid: true}
	flowlensOnly := newTask("FlowLens only", memberUUID, pgtype.Int8{})
	gitlabOnly := newTask("GitLab only", pgtype.UUID{}, pgtype.Int8{Int64: 4242, Valid: true})
	nobodys := newTask("Nobody's", pgtype.UUID{}, pgtype.Int8{})

	titles := func(rows []db.Task) []uuid.UUID {
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		return ids
	}

	t.Run("a user matches on either axis", func(t *testing.T) {
		rows, err := q.ListTasksByProject(ctx, db.ListTasksByProjectParams{
			ProjectID: p.ID, AssigneeUserID: memberUUID,
			LimitCount: testListLimit,
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, []uuid.UUID{flowlensOnly.ID, gitlabOnly.ID}, titles(rows))
	})

	t.Run("unassigned means assigned on neither axis", func(t *testing.T) {
		rows, err := q.ListTasksByProject(ctx, db.ListTasksByProjectParams{
			ProjectID: p.ID, AssigneeUnassigned: true,
			LimitCount: testListLimit,
		})
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{nobodys.ID}, titles(rows))
	})

	t.Run("no assignee filter returns every task", func(t *testing.T) {
		rows, err := q.ListTasksByProject(ctx, db.ListTasksByProjectParams{ProjectID: p.ID, LimitCount: testListLimit})
		require.NoError(t, err)
		assert.Len(t, rows, 3)
	})

	t.Run("a user with no identity matches on the FlowLens axis alone", func(t *testing.T) {
		rows, err := q.ListTasksByProject(ctx, db.ListTasksByProjectParams{
			ProjectID: p.ID, AssigneeUserID: pgtype.UUID{Bytes: owner.ID, Valid: true},
			LimitCount: testListLimit,
		})
		require.NoError(t, err)
		assert.Empty(t, rows, "the owner has no identity and owns no task, so neither axis can match")
	})
}

// ON DELETE SET NULL, not CASCADE: deleting a user must unassign their work,
// never delete it.
func TestTasksAssigneeUserID_SetNullOnUserDelete(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "assignee-del-owner")
	member := createUser(t, q, "assignee-del-member")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{
		OwnerUserID: owner.ID,
		Name:        fmt.Sprintf("AssigneeDel-%d", time.Now().UnixNano()),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)

	created, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: p.ID, Title: "Theirs", Labels: []string{},
		Priority: "medium", Progress: "not_started", Size: "m",
		CreatedByUserID: owner.ID,
		AssigneeUserID:  pgtype.UUID{Bytes: member.ID, Valid: true},
	})
	require.NoError(t, err)
	require.True(t, created.AssigneeUserID.Valid)

	_, err = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", member.ID)
	require.NoError(t, err)

	after, err := q.GetTaskForProject(ctx, db.GetTaskForProjectParams{ID: created.ID, ProjectID: p.ID})
	require.NoError(t, err)
	assert.False(t, after.AssigneeUserID.Valid, "the task must survive its assignee's deletion, unassigned")
	assert.Equal(t, "Theirs", after.Title)
}
