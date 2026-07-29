//go:build integration

// Integration tests exercise the real generated queries against a live
// PostgreSQL instance. Run with: make test-integration
// (requires DATABASE_URL and migrations already applied).
package database_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *db.Queries {
	t.Helper()
	pool := testPool(t)
	return db.New(pool)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := database.Connect(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestCreateUserAndSessionRoundTrip(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	// Unique username/email per run to keep tests independent.
	suffix := time.Now().UnixNano()
	username := fmt.Sprintf("integration-user-%d", suffix)
	email := fmt.Sprintf("integration-%d@example.com", suffix)

	u, err := q.CreateUser(ctx, db.CreateUserParams{
		Username:     username,
		Email:        email,
		DisplayName:  "Integration User",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	assert.Equal(t, username, u.Username)

	got, err := q.GetUserByUsernameOrEmail(ctx, email)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)

	// Create a session and resolve the user through the join query.
	_, err = q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    u.ID,
		TokenHash: "hash-" + username,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)

	row, err := q.GetUserBySessionToken(ctx, "hash-"+username)
	require.NoError(t, err)
	assert.Equal(t, username, row.User.Username)

	// Cleanup.
	require.NoError(t, q.DeleteSessionByTokenHash(ctx, "hash-"+username))
}

// createUser inserts a user with a name unique to this run, so tests stay
// independent of whatever previous runs left behind.
func createUser(t *testing.T, q *db.Queries, label string) db.User {
	t.Helper()
	suffix := fmt.Sprintf("%s-%d", label, time.Now().UnixNano())
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Username:     "integration-" + suffix,
		Email:        fmt.Sprintf("integration-%s@example.com", suffix),
		DisplayName:  "Integration User",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	return u
}

// Ownership is enforced by the WHERE clause of every project query, not by
// the caller. This exercises the real SQL: the fake querier can only mirror
// what the database actually does.
func TestProjectQueriesEnforceOwnership(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	intruder := createUser(t, q, "intruder")

	p, err := q.CreateProject(ctx, db.CreateProjectParams{
		OwnerUserID: owner.ID,
		Name:        "Alpha",
		Description: "desc",
	})
	require.NoError(t, err)

	t.Run("owner reads its own project", func(t *testing.T) {
		got, err := q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, p.ID, got.ID)
	})

	t.Run("non-owner gets no rows", func(t *testing.T) {
		_, err := q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{ID: p.ID, OwnerUserID: intruder.ID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("non-owner update writes nothing", func(t *testing.T) {
		_, err := q.UpdateProjectForOwner(ctx, db.UpdateProjectForOwnerParams{
			ID: p.ID, OwnerUserID: intruder.ID, Name: "Hijacked",
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)

		still, err := q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, "Alpha", still.Name)
	})

	t.Run("non-owner delete affects no rows", func(t *testing.T) {
		affected, err := q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: intruder.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})

	t.Run("owner delete affects one row, and a repeat affects none", func(t *testing.T) {
		affected, err := q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		require.Equal(t, int64(1), affected)

		affected, err = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})
}

// Backlogs carry no owner column of their own; ownership is enforced by the
// JOIN to projects in every single-backlog query. This exercises the real
// SQL, not the fake querier's approximation of it.
func TestBacklogQueriesEnforceOwnership(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	intruder := createUser(t, q, "intruder")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)

	b, err := q.CreateBacklog(ctx, db.CreateBacklogParams{ProjectID: p.ID, Name: "Sprint 1"})
	require.NoError(t, err)
	assert.Equal(t, int32(0), b.Position)

	t.Run("owner reads its own backlog", func(t *testing.T) {
		got, err := q.GetBacklogForOwner(ctx, db.GetBacklogForOwnerParams{ID: b.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, b.ID, got.ID)
	})

	t.Run("non-owner gets no rows", func(t *testing.T) {
		_, err := q.GetBacklogForOwner(ctx, db.GetBacklogForOwnerParams{ID: b.ID, OwnerUserID: intruder.ID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("non-owner update writes nothing", func(t *testing.T) {
		_, err := q.UpdateBacklogForOwner(ctx, db.UpdateBacklogForOwnerParams{
			ID: b.ID, OwnerUserID: intruder.ID, Name: "Hijacked",
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)

		still, err := q.GetBacklogForOwner(ctx, db.GetBacklogForOwnerParams{ID: b.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, "Sprint 1", still.Name)
	})

	t.Run("non-owner delete affects no rows", func(t *testing.T) {
		affected, err := q.DeleteBacklogForOwner(ctx, db.DeleteBacklogForOwnerParams{ID: b.ID, OwnerUserID: intruder.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})

	t.Run("owner delete affects one row, and a repeat affects none", func(t *testing.T) {
		affected, err := q.DeleteBacklogForOwner(ctx, db.DeleteBacklogForOwnerParams{ID: b.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		require.Equal(t, int64(1), affected)

		affected, err = q.DeleteBacklogForOwner(ctx, db.DeleteBacklogForOwnerParams{ID: b.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})
}

// Deleting a backlog must never delete its tasks: the schema's
// ON DELETE SET NULL on tasks.backlog_id drops them to unfiled (未分類)
// instead. Tasks have no generated queries yet (a later issue), so this
// inserts directly with the pool to exercise the real FK behaviour.
func TestDeleteBacklogForOwner_TasksBecomeUnfiled(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	b, err := q.CreateBacklog(ctx, db.CreateBacklogParams{ProjectID: p.ID, Name: "Sprint 1"})
	require.NoError(t, err)

	var taskID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (project_id, backlog_id, title, created_by_user_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		p.ID, b.ID, "Do the thing", owner.ID,
	).Scan(&taskID)
	require.NoError(t, err)

	affected, err := q.DeleteBacklogForOwner(ctx, db.DeleteBacklogForOwnerParams{ID: b.ID, OwnerUserID: owner.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	var backlogID pgtype.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT backlog_id FROM tasks WHERE id = $1`, taskID).Scan(&backlogID))
	assert.False(t, backlogID.Valid, "expected the task's backlog_id to become NULL (未分類), not be deleted")

	// Cleanup: the task outlives its backlog, so it is not removed by cascade.
	_, err = pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, taskID)
	require.NoError(t, err)
}

// The GitLab access token must never reach the database in plaintext (see
// the Security section of docs/plans/issue-sync.md and internal/crypto).
// This drives the real encrypted_token BYTEA column, not the fake querier's
// approximation of it, and also exercises ownership enforcement and the
// ON CONFLICT (project_id) upsert real ownership through the JOIN queries.
func TestGitlabConnectionQueries_TokenIsStoredEncrypted(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	cipher, err := crypto.New([]byte("01234567890123456789012345678901"[:32]))
	require.NoError(t, err)

	owner := createUser(t, q, "owner")
	intruder := createUser(t, q, "intruder")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})

	const plaintextToken = "glpat-super-secret-token-value"
	encrypted, err := cipher.Encrypt(plaintextToken)
	require.NoError(t, err)

	created, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           p.ID,
		BaseUrl:             "https://gitlab.example.com",
		EncryptedToken:      encrypted,
		TokenGitlabUserID:   pgtype.Int8{Int64: 99, Valid: true},
		TokenGitlabUsername: "octocat",
	})
	require.NoError(t, err)

	t.Run("the stored bytes are not the plaintext token", func(t *testing.T) {
		var raw []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT encrypted_token FROM gitlab_connections WHERE id = $1`, created.ID).Scan(&raw))
		assert.NotEqual(t, []byte(plaintextToken), raw)
		assert.NotContains(t, string(raw), plaintextToken)

		decrypted, err := cipher.Decrypt(raw)
		require.NoError(t, err)
		assert.Equal(t, plaintextToken, decrypted, "the cipher must still recover the original token")
	})

	t.Run("owner reads its own connection", func(t *testing.T) {
		got, err := q.GetGitlabConnectionForOwner(ctx, db.GetGitlabConnectionForOwnerParams{ProjectID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, created.ID, got.ID)
	})

	t.Run("non-owner gets no rows", func(t *testing.T) {
		_, err := q.GetGitlabConnectionForOwner(ctx, db.GetGitlabConnectionForOwnerParams{ProjectID: p.ID, OwnerUserID: intruder.ID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("non-owner delete affects no rows", func(t *testing.T) {
		affected, err := q.DeleteGitlabConnectionForOwner(ctx, db.DeleteGitlabConnectionForOwnerParams{ProjectID: p.ID, OwnerUserID: intruder.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})

	t.Run("re-saving upserts in place instead of duplicating the row", func(t *testing.T) {
		reEncrypted, err := cipher.Encrypt("glpat-rotated-token-value")
		require.NoError(t, err)
		updated, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
			ProjectID:           p.ID,
			BaseUrl:             "https://gitlab.example.com",
			EncryptedToken:      reEncrypted,
			TokenGitlabUserID:   pgtype.Int8{Int64: 99, Valid: true},
			TokenGitlabUsername: "octocat",
		})
		require.NoError(t, err)
		assert.Equal(t, created.ID, updated.ID, "upsert must replace the single row, not insert a second one")
	})

	t.Run("owner delete affects one row, and a repeat affects none", func(t *testing.T) {
		affected, err := q.DeleteGitlabConnectionForOwner(ctx, db.DeleteGitlabConnectionForOwnerParams{ProjectID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		require.Equal(t, int64(1), affected)

		affected, err = q.DeleteGitlabConnectionForOwner(ctx, db.DeleteGitlabConnectionForOwnerParams{ProjectID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})
}

// seedLinkedGitlabProject links a GitLab connection to a fake GitLab
// project, for tests that only care about linked_gitlab_projects itself.
func seedLinkedGitlabProject(t *testing.T, q *db.Queries, connID uuid.UUID, gitlabProjectID int64) db.LinkedGitlabProject {
	t.Helper()
	l, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: connID,
		GitlabProjectID:    gitlabProjectID,
		PathWithNamespace:  fmt.Sprintf("group/demo-%d", gitlabProjectID),
		Name:               fmt.Sprintf("demo-%d", gitlabProjectID),
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)
	return l
}

func TestLinkedGitlabProjectQueries_DefaultPromotionAndOwnership(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	intruder := createUser(t, q, "intruder")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})

	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           p.ID,
		BaseUrl:             "https://gitlab.example.com",
		EncryptedToken:      []byte("ciphertext"),
		TokenGitlabUserID:   pgtype.Int8{Int64: 99, Valid: true},
		TokenGitlabUsername: "octocat",
	})
	require.NoError(t, err)

	first := seedLinkedGitlabProject(t, q, conn.ID, 1)
	assert.True(t, first.IsDefault, "the first project linked to a connection must become its default")

	second := seedLinkedGitlabProject(t, q, conn.ID, 2)
	assert.False(t, second.IsDefault, "a second link must not also be default")

	t.Run("linking the same gitlab project twice is a conflict", func(t *testing.T) {
		_, err := q.CreateLinkedGitlabProject(ctx, db.CreateLinkedGitlabProjectParams{
			GitlabConnectionID: conn.ID,
			GitlabProjectID:    1,
			PathWithNamespace:  "group/demo-1",
			Name:               "demo-1",
			SyncScope:          "all",
			SyncLabels:         []string{},
		})
		assert.Error(t, err)
	})

	t.Run("non-owner cannot read or write a link", func(t *testing.T) {
		_, err := q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: first.ID, OwnerUserID: intruder.ID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)

		_, err = q.UpdateLinkedGitlabProjectSyncScopeForOwner(ctx, db.UpdateLinkedGitlabProjectSyncScopeForOwnerParams{
			ID: first.ID, OwnerUserID: intruder.ID, SyncScope: "labels", SyncLabels: []string{"bug"},
		})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("deleting the default link promotes the oldest remaining one", func(t *testing.T) {
		deleted, err := q.DeleteLinkedGitlabProjectForOwner(ctx, db.DeleteLinkedGitlabProjectForOwnerParams{ID: first.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.True(t, deleted.IsDefault)

		require.NoError(t, q.PromoteOldestLinkedGitlabProjectAsDefault(ctx, conn.ID))

		got, err := q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: second.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.True(t, got.IsDefault, "the only remaining link must become the default")
	})
}

// Unlinking a GitLab project must never delete the app's own tasks: only
// the sync bookkeeping (task_gitlab_links, via its FK to
// linked_gitlab_projects) is cleaned up. task_gitlab_links has no Go
// queries yet (sync isn't wired up until docs/plans/issue-sync.md phase 4+),
// so this exercises the schema's ON DELETE behaviour directly with SQL.
func TestDeletingLinkedGitlabProjectKeepsTasksButRemovesTheGitlabLink(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})

	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           p.ID,
		BaseUrl:             "https://gitlab.example.com",
		EncryptedToken:      []byte("ciphertext"),
		TokenGitlabUserID:   pgtype.Int8{Int64: 99, Valid: true},
		TokenGitlabUsername: "octocat",
	})
	require.NoError(t, err)
	link := seedLinkedGitlabProject(t, q, conn.ID, 1)

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID:       p.ID,
		Title:           "Task synced with GitLab",
		Labels:          []string{},
		CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO task_gitlab_links (task_id, linked_gitlab_project_id, gitlab_issue_id, gitlab_issue_iid)
		VALUES ($1, $2, $3, $4)`, task.ID, link.ID, int64(101), int64(7))
	require.NoError(t, err)

	_, err = q.DeleteLinkedGitlabProjectForOwner(ctx, db.DeleteLinkedGitlabProjectForOwnerParams{ID: link.ID, OwnerUserID: owner.ID})
	require.NoError(t, err)

	stillThere, err := q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: task.ID, OwnerUserID: owner.ID})
	require.NoError(t, err, "the task must survive unlinking its GitLab project")
	assert.Equal(t, task.ID, stillThere.ID)

	var linkCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM task_gitlab_links WHERE task_id = $1`, task.ID).Scan(&linkCount))
	assert.Equal(t, 0, linkCount, "the gitlab sync link row must be gone")
}

// A duplicate GitLab webhook delivery — the same linked_gitlab_project_id +
// delivery_uuid — must be a no-op, not an error, so the receiver
// (internal/webhookevent) can always respond 200 whether or not this is a
// GitLab redelivery. This drives the real UNIQUE constraint, which the fake
// querier can only approximate.
func TestCreateWebhookEvent_DuplicateDeliveryIsNoOp(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})

	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           p.ID,
		BaseUrl:             "https://gitlab.example.com",
		EncryptedToken:      []byte("ciphertext"),
		TokenGitlabUserID:   pgtype.Int8{Int64: 99, Valid: true},
		TokenGitlabUsername: "octocat",
	})
	require.NoError(t, err)
	link := seedLinkedGitlabProject(t, q, conn.ID, 1)

	params := db.CreateWebhookEventParams{
		LinkedGitlabProjectID: link.ID,
		DeliveryUuid:          "delivery-uuid-1",
		EventName:             "Issue Hook",
		ObjectKind:            "issue",
		GitlabIssueIid:        pgtype.Int8{Int64: 7, Valid: true},
		Payload:               []byte(`{"object_kind":"issue"}`),
		Status:                "pending",
	}

	first, err := q.CreateWebhookEvent(ctx, params)
	require.NoError(t, err)

	_, err = q.CreateWebhookEvent(ctx, params)
	assert.ErrorIs(t, err, pgx.ErrNoRows, "a duplicate delivery_uuid for the same link must report zero rows, not an error")

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM webhook_events WHERE id = $1`, first.ID).Scan(&count))
	assert.Equal(t, 1, count, "exactly one row must exist for the delivery")
}
