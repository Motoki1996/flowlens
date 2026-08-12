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
	// CreateProject alone leaves no project_members row (see
	// TestProjectMembers_BackfilledOwnerRow); project.Service.Create adds
	// one, but this test exercises the raw db.Queries layer directly.
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
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
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)

	b, err := q.CreateBacklog(ctx, db.CreateBacklogParams{ProjectID: p.ID, Name: "Sprint 1", Priority: "medium", Progress: "not_started"})
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
// ON DELETE SET NULL on tasks.backlog_id drops them to unfiled (Unclassified)
// instead. Tasks have no generated queries yet (a later issue), so this
// inserts directly with the pool to exercise the real FK behaviour.
func TestDeleteBacklogForOwner_TasksBecomeUnfiled(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)
	b, err := q.CreateBacklog(ctx, db.CreateBacklogParams{ProjectID: p.ID, Name: "Sprint 1", Priority: "medium", Progress: "not_started"})
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
	assert.False(t, backlogID.Valid, "expected the task's backlog_id to become NULL (Unclassified), not be deleted")

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
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
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
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
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
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
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
		Priority:        "medium",
		Progress:        "not_started",
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

// ListTasksForMember (GET /api/v1/tasks, issue #76, membership-based since
// issue #99) is hand-written rather than sqlc-generated in this branch,
// since sqlc wasn't runnable in the environment that authored it — this
// integration test is the one thing that actually proves the raw SQL parses
// and does what its doc comment claims against a real Postgres, rather than
// only the fake querier's approximation of it.
func TestListTasksForMember(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	intruder := createUser(t, q, "intruder")
	alpha, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: fmt.Sprintf("Alpha-%d", time.Now().UnixNano())})
	require.NoError(t, err)
	beta, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: fmt.Sprintf("Beta-%d", time.Now().UnixNano())})
	require.NoError(t, err)
	theirs, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: intruder.ID, Name: fmt.Sprintf("Theirs-%d", time.Now().UnixNano())})
	require.NoError(t, err)
	// CreateProject alone leaves no project_members row (see
	// TestProjectMembers_BackfilledOwnerRow) — project.Service.Create adds
	// one, but this test exercises the raw db.Queries layer directly, so it
	// must add its own, the same way SeedProject does in the fake querier.
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: alpha.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: beta.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: theirs.ID, UserID: intruder.ID, Role: "owner"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: alpha.ID, OwnerUserID: owner.ID})
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: beta.ID, OwnerUserID: owner.ID})
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: theirs.ID, OwnerUserID: intruder.ID})
	})

	soon := time.Now().AddDate(0, 0, 1)
	later := time.Now().AddDate(0, 0, 10)

	inAlpha, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: alpha.ID, Title: "In alpha, due soon", Labels: []string{}, Priority: "urgent", Progress: "not_started",
		DueOn: pgtype.Date{Time: soon, Valid: true}, CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)
	inBeta, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: beta.ID, Title: "In beta, due later", Labels: []string{}, Priority: "low", Progress: "in_progress",
		DueOn: pgtype.Date{Time: later, Valid: true}, CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)
	_, err = q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: theirs.ID, Title: "Not the owner's", Labels: []string{}, Priority: "urgent", Progress: "not_started", CreatedByUserID: intruder.ID,
	})
	require.NoError(t, err)

	t.Run("spans every owned project, carries project_name, and never leaks another owner's task", func(t *testing.T) {
		rows, err := q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
			OwnerUserID: owner.ID, ProjectIds: []uuid.UUID{}, Sort: "dueOn", LimitCount: 50,
		})
		require.NoError(t, err)

		byID := map[uuid.UUID]db.ListTasksForMemberRow{}
		for _, r := range rows {
			byID[r.ID] = r
		}
		require.Contains(t, byID, inAlpha.ID)
		require.Contains(t, byID, inBeta.ID)
		assert.Equal(t, alpha.Name, byID[inAlpha.ID].ProjectName)
		assert.Equal(t, beta.Name, byID[inBeta.ID].ProjectName)

		for _, r := range rows {
			assert.NotEqual(t, "Not the owner's", r.Title, "the intruder's task must never appear")
		}
	})

	t.Run("priority filter narrows to one task", func(t *testing.T) {
		rows, err := q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
			OwnerUserID: owner.ID, Priority: "low", ProjectIds: []uuid.UUID{}, Sort: "dueOn", LimitCount: 50,
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, inBeta.ID, rows[0].ID)
	})

	t.Run("progress filter narrows to one task", func(t *testing.T) {
		rows, err := q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
			OwnerUserID: owner.ID, Progress: "in_progress", ProjectIds: []uuid.UUID{}, Sort: "dueOn", LimitCount: 50,
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "In beta, due later", rows[0].Title)
	})

	// Progress ranks the other way from priority: not_started first.
	t.Run("sort=progress ranks not_started first regardless of due date", func(t *testing.T) {
		rows, err := q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
			OwnerUserID: owner.ID, ProjectIds: []uuid.UUID{}, Sort: "progress", LimitCount: 50,
		})
		require.NoError(t, err)
		require.Len(t, rows, 2)
		assert.Equal(t, "In alpha, due soon", rows[0].Title)
		assert.Equal(t, "In beta, due later", rows[1].Title)
	})

	t.Run("sort=priority ranks urgent first regardless of due date", func(t *testing.T) {
		rows, err := q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
			OwnerUserID: owner.ID, ProjectIds: []uuid.UUID{}, Sort: "priority", LimitCount: 50,
		})
		require.NoError(t, err)
		require.Len(t, rows, 2)
		assert.Equal(t, inAlpha.ID, rows[0].ID, "urgent must rank ahead of low despite its earlier due date")
	})

	t.Run("project_ids narrows within the caller's own projects, never a way to reach someone else's", func(t *testing.T) {
		rows, err := q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
			OwnerUserID: owner.ID, ProjectIds: []uuid.UUID{theirs.ID}, Sort: "dueOn", LimitCount: 50,
		})
		require.NoError(t, err)
		assert.Empty(t, rows)
	})
}

// TestTasksSearchVector (issue #106) drives tasks.search_vector — the
// 'simple'-config generated column the 000016 migration adds — against a
// real Postgres, since neither FakeQuerier's substring approximation nor a
// domain test can prove the tsvector match or the GIN index actually work.
func TestTasksSearchVector(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: fmt.Sprintf("Alpha-%d", time.Now().UnixNano())})
	require.NoError(t, err)
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})

	titleHit, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: p.ID, Title: "Fix login bug", Labels: []string{}, Priority: "medium", Progress: "not_started", CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)
	descriptionHit, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: p.ID, Title: "Unrelated", Description: "investigate login timeout", Labels: []string{}, Priority: "medium", Progress: "not_started", CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)
	japanese, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: p.ID, Title: "ログイン画面の修正", Labels: []string{}, Priority: "medium", Progress: "not_started", CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)
	_, err = q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID: p.ID, Title: "Something else entirely", Labels: []string{}, Priority: "medium", Progress: "not_started", CreatedByUserID: owner.ID,
	})
	require.NoError(t, err)

	baseParams := db.ListTasksByProjectParams{OwnerUserID: owner.ID, ProjectID: p.ID}

	t.Run("hits on a title match", func(t *testing.T) {
		params := baseParams
		params.Q = "login"
		rows, err := q.ListTasksByProject(ctx, params)
		require.NoError(t, err)
		var ids []uuid.UUID
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		assert.ElementsMatch(t, []uuid.UUID{titleHit.ID, descriptionHit.ID}, ids)
	})

	t.Run("hits on a description-only match", func(t *testing.T) {
		params := baseParams
		params.Q = "timeout"
		rows, err := q.ListTasksByProject(ctx, params)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, descriptionHit.ID, rows[0].ID)
	})

	t.Run("hits on a Japanese title", func(t *testing.T) {
		params := baseParams
		params.Q = "ログイン画面の修正"
		rows, err := q.ListTasksByProject(ctx, params)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, japanese.ID, rows[0].ID)
	})

	t.Run("no match returns nothing", func(t *testing.T) {
		params := baseParams
		params.Q = "no-such-word"
		rows, err := q.ListTasksByProject(ctx, params)
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	// EXPLAIN with the planner's own cost estimates would flip to a seq scan
	// on a table this small; enable_seqscan=off (rolled back with the
	// transaction) instead asks "is the GIN index usable for this query at
	// all", which is what the completion condition (#106) actually needs
	// proven.
	t.Run("the GIN index is usable for a search_vector match", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err = tx.Exec(ctx, `SET LOCAL enable_seqscan = off`)
		require.NoError(t, err)

		rows, err := tx.Query(ctx, `
			EXPLAIN SELECT id FROM tasks
			WHERE search_vector @@ websearch_to_tsquery('simple', $1)`,
			"login")
		require.NoError(t, err)
		defer rows.Close()

		var plan string
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			plan += line + "\n"
		}
		require.NoError(t, rows.Err())
		assert.Contains(t, plan, "idx_tasks_search_vector", "expected the query planner to be able to use the GIN index:\n%s", plan)
	})
}

// The 000012_project_members migration backfills an 'owner' membership row
// for every existing project's owner_user_id (docs/decisions/0010-why-project-membership.md).
// This proves that invariant against the real migration output, not just a
// freshly-created project — it also exercises project_members.sql's own
// queries end to end since nothing else calls them yet.
func TestProjectMembers_BackfilledOwnerRow(t *testing.T) {
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: fmt.Sprintf("Alpha-%d", time.Now().UnixNano())})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
	})

	t.Run("a project created after the migration still gets no automatic row (backfill is a one-time INSERT, not a trigger)", func(t *testing.T) {
		_, err := q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{ProjectID: p.ID, UserID: owner.ID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	// The 000012 migration's own backfill INSERT ran once, at migrate-up
	// time, against whatever projects existed then; other integration tests
	// sharing this database create projects afterwards without a membership
	// row (nothing wires that until a follow-up issue), so asserting the
	// invariant against live table state would be asserting something the
	// schema was never meant to guarantee going forward. Re-running the
	// migration's exact statement in a rolled-back transaction instead
	// proves the backfill logic itself is correct, independent of what
	// other tests have left lying around.
	t.Run("the migration's backfill statement gives every project an owner membership row", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()

		// A temp table, not the live project_members, so this never locks
		// against concurrent tests writing real membership rows (issue #99
		// made project.Service.Create do exactly that on every project
		// creation, which turned a `DELETE FROM project_members` here into a
		// frequent deadlock against the rest of this package's parallel
		// tests).
		_, err = tx.Exec(ctx, `CREATE TEMP TABLE tmp_project_members (LIKE project_members INCLUDING ALL) ON COMMIT DROP`)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, `INSERT INTO tmp_project_members (project_id, user_id, role) SELECT id, owner_user_id, 'owner' FROM projects`)
		require.NoError(t, err)

		var missing int
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT count(*) FROM projects p
			WHERE NOT EXISTS (
				SELECT 1 FROM tmp_project_members pm
				WHERE pm.project_id = p.id AND pm.user_id = p.owner_user_id AND pm.role = 'owner'
			)`).Scan(&missing))
		assert.Equal(t, 0, missing, "the backfill statement must give every project an owner membership row")
	})

	t.Run("AddProjectMember, GetProjectMemberRole, UpdateProjectMemberRole and RemoveProjectMember round-trip", func(t *testing.T) {
		member := createUser(t, q, "member")

		added, err := q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: member.ID, Role: "member"})
		require.NoError(t, err)
		assert.Equal(t, "member", added.Role)

		role, err := q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{ProjectID: p.ID, UserID: member.ID})
		require.NoError(t, err)
		assert.Equal(t, "member", role)

		rows, err := q.ListProjectMembers(ctx, p.ID)
		require.NoError(t, err)
		assert.Len(t, rows, 1)

		updated, err := q.UpdateProjectMemberRole(ctx, db.UpdateProjectMemberRoleParams{ProjectID: p.ID, UserID: member.ID, Role: "viewer"})
		require.NoError(t, err)
		assert.Equal(t, "viewer", updated.Role)

		affected, err := q.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{ProjectID: p.ID, UserID: member.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(1), affected)
	})
}

// The invite form's candidate search (issue #140) is the one membership query
// whose whole point is what it *excludes*: users the caller shares no project
// with, the caller themselves, and the target project's existing members. The
// fake querier can only approximate its DISTINCT/subquery/ILIKE shape, so the
// scope rule is proven here against real SQL.
func TestSearchProjectMemberCandidates_ScopeIsSharedProjectsOnly(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	// Every user in this test shares one token in their username, so the
	// search term can never match rows other integration tests left behind.
	token := fmt.Sprintf("cand%d", time.Now().UnixNano())
	newUser := func(label string) db.User {
		u, err := q.CreateUser(ctx, db.CreateUserParams{
			Username:     fmt.Sprintf("integration-%s-%s", token, label),
			Email:        fmt.Sprintf("integration-%s-%s@example.com", token, label),
			DisplayName:  "Integration User",
			PasswordHash: "bcrypt-hash",
		})
		require.NoError(t, err)
		return u
	}
	newProject := func(owner db.User, name string) db.Project {
		p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: fmt.Sprintf("%s-%s", name, token)})
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		})
		_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
		require.NoError(t, err)
		return p
	}

	caller := newUser("caller")
	colleague := newUser("colleague")
	existing := newUser("existing")
	stranger := newUser("stranger")

	target := newProject(caller, "Alpha")
	other := newProject(caller, "Beta")
	_, err := q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: other.ID, UserID: colleague.ID, Role: "member"})
	require.NoError(t, err)
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: target.ID, UserID: existing.ID, Role: "viewer"})
	require.NoError(t, err)
	newProject(stranger, "Gamma")

	rows, err := q.SearchProjectMemberCandidates(ctx, db.SearchProjectMemberCandidatesParams{
		CallerUserID: caller.ID,
		ProjectID:    target.ID,
		Query:        "%" + token + "%",
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the colleague from a shared project is a candidate")
	assert.Equal(t, colleague.ID, rows[0].ID)
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
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
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
