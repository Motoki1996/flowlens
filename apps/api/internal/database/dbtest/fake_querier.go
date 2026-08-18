// Package dbtest provides an in-memory db.Querier implementation for unit
// tests, so use cases can be tested without a real PostgreSQL instance.
package dbtest

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// FakeQuerier implements db.Querier backed by maps.
type FakeQuerier struct {
	usersByUsername map[string]db.User
	usersByEmail    map[string]db.User
	usersByID       map[uuid.UUID]db.User
	sessions        map[string]db.Session // key: token_hash

	projectAPITokens       []db.ProjectApiToken // insertion order, newest last
	projectAPITokensByID   map[uuid.UUID]db.ProjectApiToken
	projectAPITokensByHash map[string]uuid.UUID // key: token_hash

	projects            []db.Project // insertion order, newest last
	projectsByID        map[uuid.UUID]db.Project
	projectsByOwnerName map[string]db.Project // key: owner_user_id + name

	backlogs     []db.Backlog // insertion order, newest last
	backlogsByID map[uuid.UUID]db.Backlog

	tasks     []db.Task // insertion order, newest last
	tasksByID map[uuid.UUID]db.Task

	taskAIContextsByTaskID map[uuid.UUID]db.TaskAiContext

	taskDependencies     []db.TaskDependency // insertion order, newest last
	taskDependenciesByID map[uuid.UUID]db.TaskDependency

	taskComments     []db.TaskComment // insertion order, newest last
	taskCommentsByID map[uuid.UUID]db.TaskComment

	taskProgressEvents []db.TaskProgressEvent // insertion order, oldest first

	backlogProgressEvents []db.BacklogProgressEvent // insertion order, oldest first

	gitlabConnectionsByProjectID map[uuid.UUID]db.GitlabConnection
	gitlabConnectionsByID        map[uuid.UUID]db.GitlabConnection

	linkedGitlabProjects     []db.LinkedGitlabProject // insertion order, newest last
	linkedGitlabProjectsByID map[uuid.UUID]db.LinkedGitlabProject

	syncJobs            []db.SyncJob // insertion order, newest last
	syncJobsByID        map[uuid.UUID]db.SyncJob
	syncJobsByDedupeKey map[string]uuid.UUID

	taskGitlabLinksByTaskID map[uuid.UUID]db.TaskGitlabLink

	webhookEvents      []db.WebhookEvent // insertion order, newest last
	webhookEventsByKey map[string]db.WebhookEvent
	webhookEventsByID  map[uuid.UUID]db.WebhookEvent

	gitlabSyncRuns     []db.GitlabSyncRun // insertion order, newest last
	gitlabSyncRunsByID map[uuid.UUID]db.GitlabSyncRun

	repositoriesByID                    map[uuid.UUID]db.Repository
	repositoriesByLinkedGitlabProjectID map[uuid.UUID]db.Repository

	mergeRequestsByID                   map[uuid.UUID]db.MergeRequest
	mergeRequestsByGitlabMergeRequestID map[int64]uuid.UUID

	repositorySyncRuns     []db.RepositorySyncRun // insertion order, newest last
	repositorySyncRunsByID map[uuid.UUID]db.RepositorySyncRun

	projectMembers map[projectMemberKey]db.ProjectMember // key: project_id + user_id

	userGitlabIdentities map[userGitlabIdentityKey]db.UserGitlabIdentity // key: user_id + gitlab_base_url

	notificationSettingsByProjectID map[uuid.UUID]db.NotificationSetting
	notificationDigests             map[notificationDigestKey]db.NotificationDigest
}

type notificationDigestKey struct {
	projectID  uuid.UUID
	digestDate string // date-only, YYYY-MM-DD
}

type projectMemberKey struct {
	projectID uuid.UUID
	userID    uuid.UUID
}

type userGitlabIdentityKey struct {
	userID  uuid.UUID
	baseURL string
}

// New returns an empty FakeQuerier.
func New() *FakeQuerier {
	return &FakeQuerier{
		usersByUsername: map[string]db.User{},
		usersByEmail:    map[string]db.User{},
		usersByID:       map[uuid.UUID]db.User{},
		sessions:        map[string]db.Session{},

		projectAPITokensByID:   map[uuid.UUID]db.ProjectApiToken{},
		projectAPITokensByHash: map[string]uuid.UUID{},

		projectsByID:        map[uuid.UUID]db.Project{},
		projectsByOwnerName: map[string]db.Project{},
		backlogsByID:        map[uuid.UUID]db.Backlog{},
		tasksByID:           map[uuid.UUID]db.Task{},

		taskAIContextsByTaskID: map[uuid.UUID]db.TaskAiContext{},

		taskDependenciesByID: map[uuid.UUID]db.TaskDependency{},

		taskCommentsByID: map[uuid.UUID]db.TaskComment{},

		gitlabConnectionsByProjectID: map[uuid.UUID]db.GitlabConnection{},
		gitlabConnectionsByID:        map[uuid.UUID]db.GitlabConnection{},

		linkedGitlabProjectsByID: map[uuid.UUID]db.LinkedGitlabProject{},

		syncJobsByID:        map[uuid.UUID]db.SyncJob{},
		syncJobsByDedupeKey: map[string]uuid.UUID{},

		taskGitlabLinksByTaskID: map[uuid.UUID]db.TaskGitlabLink{},

		webhookEventsByKey: map[string]db.WebhookEvent{},
		webhookEventsByID:  map[uuid.UUID]db.WebhookEvent{},

		gitlabSyncRunsByID: map[uuid.UUID]db.GitlabSyncRun{},

		repositoriesByID:                    map[uuid.UUID]db.Repository{},
		repositoriesByLinkedGitlabProjectID: map[uuid.UUID]db.Repository{},

		mergeRequestsByID:                   map[uuid.UUID]db.MergeRequest{},
		mergeRequestsByGitlabMergeRequestID: map[int64]uuid.UUID{},

		repositorySyncRunsByID: map[uuid.UUID]db.RepositorySyncRun{},

		projectMembers: map[projectMemberKey]db.ProjectMember{},

		userGitlabIdentities: map[userGitlabIdentityKey]db.UserGitlabIdentity{},

		notificationSettingsByProjectID: map[uuid.UUID]db.NotificationSetting{},
		notificationDigests:             map[notificationDigestKey]db.NotificationDigest{},
	}
}

func now() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
}

// roleRank orders project_members.role the same way project.Role does:
// viewer < member < owner. A role missing from this map (i.e. no membership
// row at all) ranks below every real role.
var roleRank = map[string]int{"viewer": 1, "member": 2, "owner": 3}

// hasMembership reports whether userID has any project_members row at all
// for projectID, mirroring an unrestricted "EXISTS (... pm.user_id = $N)"
// subquery — the read-tier (viewer-minimum) check every *ForOwner query
// below uses.
func (f *FakeQuerier) hasMembership(projectID, userID uuid.UUID) bool {
	_, ok := f.projectMembers[projectMemberKey{projectID, userID}]
	return ok
}

// hasRoleAtLeast reports whether userID's project_members role for
// projectID is at least min ("member" or "owner"), mirroring an
// "EXISTS (... AND pm.role IN (...))" subquery — the write-tier check.
func (f *FakeQuerier) hasRoleAtLeast(projectID, userID uuid.UUID, min string) bool {
	m, ok := f.projectMembers[projectMemberKey{projectID, userID}]
	if !ok {
		return false
	}
	return roleRank[m.Role] >= roleRank[min]
}

func projectOwnerNameKey(ownerID uuid.UUID, name string) string {
	return ownerID.String() + "\x00" + name
}

// SeedUser inserts a ready-made user directly, bypassing password hashing.
// Use it in tests that need a pre-existing user but don't exercise sign-up
// (e.g. duplicate-constraint or session tests). Returns the stored row.
func (f *FakeQuerier) SeedUser(username, email string) db.User {
	u := db.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		DisplayName:  username,
		PasswordHash: "seeded-hash",
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}
	f.usersByUsername[u.Username] = u
	f.usersByEmail[u.Email] = u
	f.usersByID[u.ID] = u
	return u
}

func (f *FakeQuerier) CreateUser(_ context.Context, arg db.CreateUserParams) (db.User, error) {
	if _, ok := f.usersByUsername[arg.Username]; ok {
		return db.User{}, &pgconn.PgError{Code: "23505", ConstraintName: "users_username_key"}
	}
	if _, ok := f.usersByEmail[arg.Email]; ok {
		return db.User{}, &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}
	}
	u := db.User{
		ID:           uuid.New(),
		Username:     arg.Username,
		Email:        arg.Email,
		DisplayName:  arg.DisplayName,
		PasswordHash: arg.PasswordHash,
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}
	f.usersByUsername[u.Username] = u
	f.usersByEmail[u.Email] = u
	f.usersByID[u.ID] = u
	return u, nil
}

func (f *FakeQuerier) GetUserByUsernameOrEmail(_ context.Context, username string) (db.User, error) {
	if u, ok := f.usersByUsername[username]; ok {
		return u, nil
	}
	if u, ok := f.usersByEmail[username]; ok {
		return u, nil
	}
	return db.User{}, pgx.ErrNoRows
}

func (f *FakeQuerier) GetUserByID(_ context.Context, id uuid.UUID) (db.User, error) {
	u, ok := f.usersByID[id]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (f *FakeQuerier) CreateSession(_ context.Context, arg db.CreateSessionParams) (db.Session, error) {
	s := db.Session{
		ID:        uuid.New(),
		UserID:    arg.UserID,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		CreatedAt: now(),
	}
	f.sessions[arg.TokenHash] = s
	return s, nil
}

func (f *FakeQuerier) GetUserBySessionToken(_ context.Context, tokenHash string) (db.GetUserBySessionTokenRow, error) {
	s, ok := f.sessions[tokenHash]
	if !ok || !s.ExpiresAt.Time.After(time.Now()) {
		return db.GetUserBySessionTokenRow{}, pgx.ErrNoRows
	}
	u, ok := f.usersByID[s.UserID]
	if !ok {
		return db.GetUserBySessionTokenRow{}, pgx.ErrNoRows
	}
	return db.GetUserBySessionTokenRow{User: u}, nil
}

func (f *FakeQuerier) DeleteSessionByTokenHash(_ context.Context, tokenHash string) error {
	delete(f.sessions, tokenHash)
	return nil
}

func (f *FakeQuerier) DeleteExpiredSessions(_ context.Context) error {
	for k, s := range f.sessions {
		if !s.ExpiresAt.Time.After(time.Now()) {
			delete(f.sessions, k)
		}
	}
	return nil
}

// storeProjectAPIToken inserts t if it is new, or overwrites the existing
// row in place (preserving its position in f.projectAPITokens) otherwise.
func (f *FakeQuerier) storeProjectAPIToken(t db.ProjectApiToken) {
	f.projectAPITokensByID[t.ID] = t
	f.projectAPITokensByHash[t.TokenHash] = t.ID
	for i, x := range f.projectAPITokens {
		if x.ID == t.ID {
			f.projectAPITokens[i] = t
			return
		}
	}
	f.projectAPITokens = append(f.projectAPITokens, t)
}

func (f *FakeQuerier) CreateProjectAPIToken(_ context.Context, arg db.CreateProjectAPITokenParams) (db.ProjectApiToken, error) {
	t := db.ProjectApiToken{
		ID:          uuid.New(),
		ProjectID:   arg.ProjectID,
		Name:        arg.Name,
		TokenHash:   arg.TokenHash,
		TokenPrefix: arg.TokenPrefix,
		Scopes:      arg.Scopes,
		ExpiresAt:   arg.ExpiresAt,
		CreatedAt:   now(),
	}
	f.storeProjectAPIToken(t)
	return t, nil
}

// ListProjectAPITokensByProject mirrors the SQL's ORDER BY created_at DESC.
func (f *FakeQuerier) ListProjectAPITokensByProject(_ context.Context, projectID uuid.UUID) ([]db.ProjectApiToken, error) {
	items := []db.ProjectApiToken{}
	for i := len(f.projectAPITokens) - 1; i >= 0; i-- {
		if t := f.projectAPITokens[i]; t.ProjectID == projectID {
			items = append(items, t)
		}
	}
	return items, nil
}

// projectAPITokenOwner returns the owner_user_id of the project a token
// belongs to, mirroring the JOIN the real query performs.
func (f *FakeQuerier) projectAPITokenOwner(t db.ProjectApiToken) (uuid.UUID, bool) {
	p, ok := f.projectsByID[t.ProjectID]
	if !ok {
		return uuid.Nil, false
	}
	return p.OwnerUserID, true
}

// DeleteProjectAPITokenForOwner returns the number of rows affected, so
// callers can tell "deleted" from "not yours / not there" exactly as
// Postgres does.
func (f *FakeQuerier) DeleteProjectAPITokenForOwner(_ context.Context, arg db.DeleteProjectAPITokenForOwnerParams) (int64, error) {
	t, ok := f.projectAPITokensByID[arg.ID]
	if !ok {
		return 0, nil
	}
	owner, ok := f.projectAPITokenOwner(t)
	if !ok || owner != arg.OwnerUserID {
		return 0, nil
	}
	delete(f.projectAPITokensByID, t.ID)
	delete(f.projectAPITokensByHash, t.TokenHash)
	for i, x := range f.projectAPITokens {
		if x.ID == t.ID {
			f.projectAPITokens = append(f.projectAPITokens[:i], f.projectAPITokens[i+1:]...)
			break
		}
	}
	return 1, nil
}

// GetProjectAPITokenByTokenHash mirrors the SQL: a token that does not exist
// or has expired is reported as pgx.ErrNoRows, exactly like an unknown hash.
// Like the real JOIN, a token whose project has been deleted (should not
// happen given the FK's ON DELETE CASCADE, but the fake has no such
// constraint) is also reported as pgx.ErrNoRows.
func (f *FakeQuerier) GetProjectAPITokenByTokenHash(_ context.Context, tokenHash string) (db.GetProjectAPITokenByTokenHashRow, error) {
	id, ok := f.projectAPITokensByHash[tokenHash]
	if !ok {
		return db.GetProjectAPITokenByTokenHashRow{}, pgx.ErrNoRows
	}
	t := f.projectAPITokensByID[id]
	if t.ExpiresAt.Valid && !t.ExpiresAt.Time.After(time.Now()) {
		return db.GetProjectAPITokenByTokenHashRow{}, pgx.ErrNoRows
	}
	owner, ok := f.projectAPITokenOwner(t)
	if !ok {
		return db.GetProjectAPITokenByTokenHashRow{}, pgx.ErrNoRows
	}
	return db.GetProjectAPITokenByTokenHashRow{
		ID:          t.ID,
		ProjectID:   t.ProjectID,
		Name:        t.Name,
		TokenHash:   t.TokenHash,
		LastUsedAt:  t.LastUsedAt,
		ExpiresAt:   t.ExpiresAt,
		CreatedAt:   t.CreatedAt,
		Scopes:      t.Scopes,
		TokenPrefix: t.TokenPrefix,
		OwnerUserID: owner,
	}, nil
}

func (f *FakeQuerier) UpdateProjectAPITokenLastUsedAt(_ context.Context, arg db.UpdateProjectAPITokenLastUsedAtParams) error {
	t, ok := f.projectAPITokensByID[arg.ID]
	if !ok {
		return nil
	}
	t.LastUsedAt = arg.LastUsedAt
	f.storeProjectAPIToken(t)
	return nil
}

// SeedProject inserts a ready-made project directly, bypassing validation.
// Use it in tests that need a pre-existing project but don't exercise
// creation (e.g. duplicate-name or authorization tests). Returns the stored row.
func (f *FakeQuerier) SeedProject(ownerID uuid.UUID, name string) db.Project {
	p := db.Project{
		ID:          uuid.New(),
		OwnerUserID: ownerID,
		Name:        name,
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}
	f.storeProject(p)
	// Every project has an 'owner'-role project_members row from the moment
	// it exists — migration 000012 backfilled this for projects that
	// predate project_members, and project.Service.Create does the
	// equivalent for new ones; SeedProject mirrors both so a test that only
	// calls SeedProject and acts as ownerID keeps passing unmodified
	// (docs/decisions/0010-why-project-membership.md).
	f.SeedProjectMember(p.ID, ownerID, "owner")
	return p
}

func (f *FakeQuerier) storeProject(p db.Project) {
	f.projects = append(f.projects, p)
	f.projectsByID[p.ID] = p
	f.projectsByOwnerName[projectOwnerNameKey(p.OwnerUserID, p.Name)] = p
}

func (f *FakeQuerier) CreateProject(_ context.Context, arg db.CreateProjectParams) (db.Project, error) {
	if _, ok := f.projectsByOwnerName[projectOwnerNameKey(arg.OwnerUserID, arg.Name)]; ok {
		return db.Project{}, &pgconn.PgError{Code: "23505", ConstraintName: "projects_owner_user_id_name_key"}
	}
	p := db.Project{
		ID:          uuid.New(),
		OwnerUserID: arg.OwnerUserID,
		Name:        arg.Name,
		Description: arg.Description,
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}
	f.storeProject(p)
	return p, nil
}

// GetProjectForOwner mirrors the SQL: viewer-minimum (any project_members
// role), since a caller with no membership row at all is reported as
// missing, indistinguishable from a project that doesn't exist.
func (f *FakeQuerier) GetProjectForOwner(_ context.Context, arg db.GetProjectForOwnerParams) (db.Project, error) {
	p, ok := f.projectsByID[arg.ID]
	if !ok || !f.hasMembership(arg.ID, arg.OwnerUserID) {
		return db.Project{}, pgx.ErrNoRows
	}
	return p, nil
}

// GetProjectByID is unscoped, mirroring the SQL used by the inbound webhook
// apply pipeline (internal/webhookapply), which has no acting user to check
// against.
func (f *FakeQuerier) GetProjectByID(_ context.Context, id uuid.UUID) (db.Project, error) {
	p, ok := f.projectsByID[id]
	if !ok {
		return db.Project{}, pgx.ErrNoRows
	}
	return p, nil
}

// ListProjectsByMember mirrors the SQL: every project userID is a member of,
// any role, newest first.
func (f *FakeQuerier) ListProjectsByMember(_ context.Context, userID uuid.UUID) ([]db.Project, error) {
	items := []db.Project{}
	for i := len(f.projects) - 1; i >= 0; i-- {
		if p := f.projects[i]; f.hasMembership(p.ID, userID) {
			items = append(items, p)
		}
	}
	return items, nil
}

// UpdateProjectForOwner mirrors the SQL: member-minimum.
func (f *FakeQuerier) UpdateProjectForOwner(_ context.Context, arg db.UpdateProjectForOwnerParams) (db.Project, error) {
	existing, ok := f.projectsByID[arg.ID]
	if !ok || !f.hasRoleAtLeast(arg.ID, arg.OwnerUserID, "member") {
		return db.Project{}, pgx.ErrNoRows
	}

	oldKey := projectOwnerNameKey(existing.OwnerUserID, existing.Name)
	newKey := projectOwnerNameKey(existing.OwnerUserID, arg.Name)
	if newKey != oldKey {
		if _, ok := f.projectsByOwnerName[newKey]; ok {
			return db.Project{}, &pgconn.PgError{Code: "23505", ConstraintName: "projects_owner_user_id_name_key"}
		}
		delete(f.projectsByOwnerName, oldKey)
	}

	existing.Name = arg.Name
	existing.Description = arg.Description
	existing.UpdatedAt = now()

	f.projectsByID[arg.ID] = existing
	f.projectsByOwnerName[newKey] = existing
	for i, p := range f.projects {
		if p.ID == existing.ID {
			f.projects[i] = existing
			break
		}
	}
	return existing, nil
}

// DeleteProjectForOwner returns the number of rows affected, so callers can
// tell "deleted" from "not yours / not there" exactly as Postgres does.
func (f *FakeQuerier) DeleteProjectForOwner(_ context.Context, arg db.DeleteProjectForOwnerParams) (int64, error) {
	p, ok := f.projectsByID[arg.ID]
	if !ok || p.OwnerUserID != arg.OwnerUserID {
		return 0, nil
	}
	delete(f.projectsByID, arg.ID)
	delete(f.projectsByOwnerName, projectOwnerNameKey(p.OwnerUserID, p.Name))
	for i, x := range f.projects {
		if x.ID == p.ID {
			f.projects = append(f.projects[:i], f.projects[i+1:]...)
			break
		}
	}
	return 1, nil
}

// SeedBacklog inserts a ready-made backlog directly, bypassing validation.
// Use it in tests that need a pre-existing backlog but don't exercise
// creation (e.g. authorization tests). Returns the stored row.
func (f *FakeQuerier) SeedBacklog(projectID uuid.UUID, name string) db.Backlog {
	b := db.Backlog{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      name,
		Position:  f.nextBacklogPosition(projectID),
		Priority:  "medium",
		Progress:  "not_started",
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	f.backlogs = append(f.backlogs, b)
	f.backlogsByID[b.ID] = b
	return b
}

// SeedBacklogWithCreatedAt is SeedBacklog with an explicit created_at, for
// tests (e.g. internal/flowmetrics) that need to control a backlog's age
// precisely instead of getting SeedBacklog's now().
func (f *FakeQuerier) SeedBacklogWithCreatedAt(projectID uuid.UUID, name string, createdAt time.Time) db.Backlog {
	b := f.SeedBacklog(projectID, name)
	b.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	f.backlogsByID[b.ID] = b
	for i, x := range f.backlogs {
		if x.ID == b.ID {
			f.backlogs[i] = b
		}
	}
	return b
}

func (f *FakeQuerier) nextBacklogPosition(projectID uuid.UUID) int32 {
	var max int32 = -1
	for _, b := range f.backlogs {
		if b.ProjectID == projectID && b.Position > max {
			max = b.Position
		}
	}
	return max + 1
}

func (f *FakeQuerier) CreateBacklog(_ context.Context, arg db.CreateBacklogParams) (db.Backlog, error) {
	b := db.Backlog{
		ID:                           uuid.New(),
		ProjectID:                    arg.ProjectID,
		Name:                         arg.Name,
		Description:                  arg.Description,
		Position:                     f.nextBacklogPosition(arg.ProjectID),
		StartDate:                    arg.StartDate,
		DueOn:                        arg.DueOn,
		Priority:                     arg.Priority,
		Progress:                     arg.Progress,
		DefaultLinkedGitlabProjectID: arg.DefaultLinkedGitlabProjectID,
		CreatedAt:                    now(),
		UpdatedAt:                    now(),
	}
	f.backlogs = append(f.backlogs, b)
	f.backlogsByID[b.ID] = b
	return b, nil
}

// priorityRank mirrors the SQL query's CASE expression: urgent > high >
// medium > low > anything else.
func priorityRank(priority string) int {
	switch priority {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// progressRank mirrors the SQL query's CASE expression, and runs the other
// way from priorityRank: not_started first through done, so sorting ascending
// reads as the work advancing.
func progressRank(progress string) int {
	switch progress {
	case "not_started":
		return 1
	case "in_progress":
		return 2
	case "on_hold":
		return 3
	case "done":
		return 4
	default:
		return 0
	}
}

// ListBacklogsByProject mirrors the SQL's LEFT JOIN aggregate (issue #144):
// TaskCount/ClosedTaskCount are computed from f.tasks the same way
// countFailedSyncTasks derives its own count.
func (f *FakeQuerier) ListBacklogsByProject(_ context.Context, arg db.ListBacklogsByProjectParams) ([]db.ListBacklogsByProjectRow, error) {
	items := []db.ListBacklogsByProjectRow{}
	for _, b := range f.backlogs {
		if b.ProjectID != arg.ProjectID {
			continue
		}
		if arg.Priority != "" && b.Priority != arg.Priority {
			continue
		}
		if arg.Progress != "" && b.Progress != arg.Progress {
			continue
		}
		taskCount, closedTaskCount := f.backlogTaskCounts(b.ID)
		items = append(items, db.ListBacklogsByProjectRow{
			ID:                           b.ID,
			ProjectID:                    b.ProjectID,
			Name:                         b.Name,
			Description:                  b.Description,
			Position:                     b.Position,
			CreatedAt:                    b.CreatedAt,
			UpdatedAt:                    b.UpdatedAt,
			StartDate:                    b.StartDate,
			DueOn:                        b.DueOn,
			Priority:                     b.Priority,
			Progress:                     b.Progress,
			DefaultLinkedGitlabProjectID: b.DefaultLinkedGitlabProjectID,
			TaskCount:                    taskCount,
			ClosedTaskCount:              closedTaskCount,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if arg.SortByPriority {
			if ri, rj := priorityRank(items[i].Priority), priorityRank(items[j].Priority); ri != rj {
				return ri > rj
			}
		}
		if arg.SortByProgress {
			if ri, rj := progressRank(items[i].Progress), progressRank(items[j].Progress); ri != rj {
				return ri < rj
			}
		}
		return items[i].Position < items[j].Position
	})
	return items, nil
}

// backlogTaskCounts mirrors ListBacklogsByProject's LEFT JOIN aggregate:
// backlogID's total task count and how many of those are closed.
func (f *FakeQuerier) backlogTaskCounts(backlogID uuid.UUID) (taskCount, closedTaskCount int64) {
	for _, t := range f.tasks {
		if !t.BacklogID.Valid || t.BacklogID.Bytes != backlogID {
			continue
		}
		taskCount++
		if t.Status == "closed" {
			closedTaskCount++
		}
	}
	return taskCount, closedTaskCount
}

// GetBacklogForOwner mirrors the SQL: viewer-minimum (any project_members
// role) on the backlog's project.
func (f *FakeQuerier) GetBacklogForOwner(_ context.Context, arg db.GetBacklogForOwnerParams) (db.Backlog, error) {
	b, ok := f.backlogsByID[arg.ID]
	if !ok || !f.hasMembership(b.ProjectID, arg.OwnerUserID) {
		return db.Backlog{}, pgx.ErrNoRows
	}
	return b, nil
}

// GetBacklogProjectID mirrors the SQL: no owner join, just the backlog's
// own project_id.
func (f *FakeQuerier) GetBacklogProjectID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	b, ok := f.backlogsByID[id]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return b.ProjectID, nil
}

// UpdateBacklogForOwner mirrors the SQL: member-minimum.
func (f *FakeQuerier) UpdateBacklogForOwner(_ context.Context, arg db.UpdateBacklogForOwnerParams) (db.Backlog, error) {
	existing, ok := f.backlogsByID[arg.ID]
	if !ok || !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Backlog{}, pgx.ErrNoRows
	}

	// The real UPDATE writes both dates unconditionally; backlog.Service is
	// what resolves an absent one to its current value before calling here.
	existing.Name = arg.Name
	existing.Description = arg.Description
	existing.Position = arg.Position
	existing.StartDate = arg.StartDate
	existing.DueOn = arg.DueOn
	existing.Priority = arg.Priority
	existing.Progress = arg.Progress
	existing.DefaultLinkedGitlabProjectID = arg.DefaultLinkedGitlabProjectID
	existing.UpdatedAt = now()

	f.backlogsByID[arg.ID] = existing
	for i, b := range f.backlogs {
		if b.ID == existing.ID {
			f.backlogs[i] = existing
			break
		}
	}
	return existing, nil
}

// ReorderBacklogs mirrors the SQL: resequences position 0..n-1 for every
// backlog in arg.BacklogIds belonging to arg.ProjectID, in the order given.
// A backlog ID not in that project is silently skipped, matching the real
// query's WHERE clause — internal/backlog.Service.Reorder is what guarantees
// the set matches before calling this.
func (f *FakeQuerier) ReorderBacklogs(_ context.Context, arg db.ReorderBacklogsParams) error {
	for i, id := range arg.BacklogIds {
		b, ok := f.backlogsByID[id]
		if !ok || b.ProjectID != arg.ProjectID {
			continue
		}
		b.Position = int32(i)
		b.UpdatedAt = now()
		f.backlogsByID[id] = b
		for j, x := range f.backlogs {
			if x.ID == id {
				f.backlogs[j] = b
				break
			}
		}
	}
	return nil
}

// DeleteBacklogForOwner returns the number of rows affected, so callers can
// tell "deleted" from "not yours / not there" exactly as Postgres does.
func (f *FakeQuerier) DeleteBacklogForOwner(_ context.Context, arg db.DeleteBacklogForOwnerParams) (int64, error) {
	b, ok := f.backlogsByID[arg.ID]
	if !ok || !f.hasRoleAtLeast(b.ProjectID, arg.OwnerUserID, "member") {
		return 0, nil
	}
	delete(f.backlogsByID, b.ID)
	for i, x := range f.backlogs {
		if x.ID == b.ID {
			f.backlogs = append(f.backlogs[:i], f.backlogs[i+1:]...)
			break
		}
	}
	return 1, nil
}

// SeedTask inserts a ready-made, unfiled (backlog_id = NULL) task directly,
// bypassing validation. Use it in tests that need a pre-existing task but
// don't exercise creation. Returns the stored row.
func (f *FakeQuerier) SeedTask(projectID, createdByUserID uuid.UUID, title string) db.Task {
	return f.seedTask(projectID, createdByUserID, title, pgtype.UUID{})
}

// SeedTaskInBacklog inserts a ready-made task assigned to backlogID,
// bypassing validation. Returns the stored row.
func (f *FakeQuerier) SeedTaskInBacklog(projectID, backlogID, createdByUserID uuid.UUID, title string) db.Task {
	return f.seedTask(projectID, createdByUserID, title, pgtype.UUID{Bytes: backlogID, Valid: true})
}

// SeedTaskInBacklogWithCreatedAt combines SeedTaskInBacklog and
// SeedTaskWithCreatedAt: a task filed under backlogID with an explicit
// created_at, for tests (e.g. internal/flowmetrics's task-breakdown stage)
// that need to control both.
func (f *FakeQuerier) SeedTaskInBacklogWithCreatedAt(projectID, backlogID, createdByUserID uuid.UUID, title string, createdAt time.Time) db.Task {
	t := f.seedTask(projectID, createdByUserID, title, pgtype.UUID{Bytes: backlogID, Valid: true})
	t.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	f.storeTask(t)
	return t
}

// SeedTaskWithCreatedAt is SeedTask with an explicit created_at, for tests
// (e.g. internal/flowmetrics) that need to control a task's age precisely
// instead of getting seedTask's now().
func (f *FakeQuerier) SeedTaskWithCreatedAt(projectID, createdByUserID uuid.UUID, title string, createdAt time.Time) db.Task {
	t := f.seedTask(projectID, createdByUserID, title, pgtype.UUID{})
	t.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	f.storeTask(t)
	return t
}

// SeedTaskDesignImplementationStarted sets an existing task's
// design_started_at/implementation_started_at directly, for tests (e.g.
// internal/flowmetrics) that need precise control over the two
// spec-driven-development phase markers instead of going through
// MarkTaskDesignStarted/MarkTaskImplementationStarted's now(). Either
// argument may be zero-valued (pgtype.Timestamptz{}) to leave that marker
// unset.
func (f *FakeQuerier) SeedTaskDesignImplementationStarted(taskID uuid.UUID, designStartedAt, implementationStartedAt pgtype.Timestamptz) db.Task {
	t := f.tasksByID[taskID]
	t.DesignStartedAt = designStartedAt
	t.ImplementationStartedAt = implementationStartedAt
	f.storeTask(t)
	return t
}

func (f *FakeQuerier) seedTask(projectID, createdByUserID uuid.UUID, title string, backlogID pgtype.UUID) db.Task {
	t := db.Task{
		ID:              uuid.New(),
		ProjectID:       projectID,
		BacklogID:       backlogID,
		Title:           title,
		Status:          "open",
		Labels:          []string{},
		Priority:        "medium",
		Progress:        "not_started",
		Position:        f.nextTaskPosition(projectID, backlogID),
		CreatedByUserID: createdByUserID,
		CreatedAt:       now(),
		UpdatedAt:       now(),
	}
	f.storeTask(t)
	return t
}

// storeTask inserts t if it is new, or overwrites the existing row in place
// (preserving its position in f.tasks) otherwise.
func (f *FakeQuerier) storeTask(t db.Task) {
	f.tasksByID[t.ID] = t
	for i, x := range f.tasks {
		if x.ID == t.ID {
			f.tasks[i] = t
			return
		}
	}
	f.tasks = append(f.tasks, t)
}

func (f *FakeQuerier) nextTaskPosition(projectID uuid.UUID, backlogID pgtype.UUID) int32 {
	var max int32 = -1
	for _, t := range f.tasks {
		if t.ProjectID != projectID {
			continue
		}
		if t.BacklogID.Valid != backlogID.Valid || (t.BacklogID.Valid && t.BacklogID.Bytes != backlogID.Bytes) {
			continue
		}
		if t.Position > max {
			max = t.Position
		}
	}
	return max + 1
}

func (f *FakeQuerier) CreateTask(_ context.Context, arg db.CreateTaskParams) (db.Task, error) {
	t := db.Task{
		ID:                     uuid.New(),
		ProjectID:              arg.ProjectID,
		BacklogID:              arg.BacklogID,
		Title:                  arg.Title,
		Description:            arg.Description,
		Status:                 "open",
		AssigneeGitlabUserID:   arg.AssigneeGitlabUserID,
		AssigneeGitlabUsername: arg.AssigneeGitlabUsername,
		Labels:                 arg.Labels,
		DueOn:                  arg.DueOn,
		StartDate:              arg.StartDate,
		Priority:               arg.Priority,
		Progress:               arg.Progress,
		Position:               f.nextTaskPosition(arg.ProjectID, arg.BacklogID),
		CreatedByUserID:        arg.CreatedByUserID,
		CreatedAt:              now(),
		UpdatedAt:              now(),
	}
	f.storeTask(t)
	return t, nil
}

// ApplyWebhookTaskFields mirrors the SQL: an unscoped write by task ID only,
// used by the inbound webhook apply pipeline (internal/webhookapply).
// closed_at only advances on a transition into 'closed' — re-applying while
// already closed never moves it — mirroring the real query's CASE, which
// reads the pre-update value.
func (f *FakeQuerier) ApplyWebhookTaskFields(_ context.Context, arg db.ApplyWebhookTaskFieldsParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.Title = arg.Title
	existing.Description = arg.Description
	existing.AssigneeGitlabUserID = arg.AssigneeGitlabUserID
	existing.AssigneeGitlabUsername = arg.AssigneeGitlabUsername
	existing.Labels = arg.Labels
	existing.DueOn = arg.DueOn
	existing.Status = arg.Status
	if arg.Status == "closed" {
		if !existing.ClosedAt.Valid {
			existing.ClosedAt = now()
		}
	} else {
		existing.ClosedAt = pgtype.Timestamptz{}
	}
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

// matchesTaskQuery is ListTasksByProject/ListTasksForMember's q filter,
// approximated as a case-insensitive substring match on title/description —
// good enough for domain-layer tests, which don't have a real Postgres to
// run search_vector @@ websearch_to_tsquery against (that's covered at the
// integration layer, internal/database).
func matchesTaskQuery(title, description, q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(title), q) || strings.Contains(strings.ToLower(description), q)
}

func (f *FakeQuerier) ListTasksByProject(_ context.Context, arg db.ListTasksByProjectParams) ([]db.Task, error) {
	items := []db.Task{}
	for _, t := range f.tasks {
		if t.ProjectID != arg.ProjectID {
			continue
		}
		if arg.Unassigned && t.BacklogID.Valid {
			continue
		}
		if arg.BacklogID.Valid && (!t.BacklogID.Valid || t.BacklogID.Bytes != arg.BacklogID.Bytes) {
			continue
		}
		if arg.Status != "" && t.Status != arg.Status {
			continue
		}
		if arg.Priority != "" && t.Priority != arg.Priority {
			continue
		}
		if arg.Progress != "" && t.Progress != arg.Progress {
			continue
		}
		if arg.AssigneeMe && !f.matchesAssigneeMe(t.AssigneeGitlabUserID, arg.OwnerUserID, t.ProjectID) {
			continue
		}
		if !matchesTaskQuery(t.Title, t.Description, arg.Q) {
			continue
		}
		items = append(items, t)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if arg.SortByPriority {
			if ri, rj := priorityRank(items[i].Priority), priorityRank(items[j].Priority); ri != rj {
				return ri > rj
			}
		}
		if arg.SortByProgress {
			if ri, rj := progressRank(items[i].Progress), progressRank(items[j].Progress); ri != rj {
				return ri < rj
			}
		}
		return items[i].Position < items[j].Position
	})
	return items, nil
}

// ListTasksByProjectPaged mirrors the SQL: ListTasksByProject's
// backlog_id/status filters plus updated_since and LIMIT/OFFSET paging,
// ordered the same way (position ASC, created_at ASC).
func (f *FakeQuerier) ListTasksByProjectPaged(_ context.Context, arg db.ListTasksByProjectPagedParams) ([]db.Task, error) {
	items := []db.Task{}
	for _, t := range f.tasks {
		if t.ProjectID != arg.ProjectID {
			continue
		}
		if arg.BacklogID.Valid && (!t.BacklogID.Valid || t.BacklogID.Bytes != arg.BacklogID.Bytes) {
			continue
		}
		if arg.Status != "" && t.Status != arg.Status {
			continue
		}
		if arg.UpdatedSince.Valid && t.UpdatedAt.Time.Before(arg.UpdatedSince.Time) {
			continue
		}
		items = append(items, t)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Position != items[j].Position {
			return items[i].Position < items[j].Position
		}
		return items[i].CreatedAt.Time.Before(items[j].CreatedAt.Time)
	})

	offset := int(arg.OffsetCount)
	if offset > len(items) {
		offset = len(items)
	}
	items = items[offset:]
	limit := int(arg.LimitCount)
	if limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

// earliestMergeRequestForTask mirrors ListTasksForFlowMetrics' LATERAL join:
// among merge_requests linked to taskID, the one with the earliest
// gitlab_created_at (NULLS LAST), tie-broken by created_at ASC. ok is false
// when taskID has no linked merge request at all.
func (f *FakeQuerier) earliestMergeRequestForTask(taskID uuid.UUID) (db.MergeRequest, bool) {
	var (
		best   db.MergeRequest
		found  bool
		better = func(candidate db.MergeRequest) bool {
			if !found {
				return true
			}
			if candidate.GitlabCreatedAt.Valid != best.GitlabCreatedAt.Valid {
				return candidate.GitlabCreatedAt.Valid
			}
			if candidate.GitlabCreatedAt.Valid && !candidate.GitlabCreatedAt.Time.Equal(best.GitlabCreatedAt.Time) {
				return candidate.GitlabCreatedAt.Time.Before(best.GitlabCreatedAt.Time)
			}
			return candidate.CreatedAt.Time.Before(best.CreatedAt.Time)
		}
	)
	for _, m := range f.mergeRequestsByID {
		if !m.TaskID.Valid || m.TaskID.Bytes != taskID {
			continue
		}
		if better(m) {
			best = m
			found = true
		}
	}
	return best, found
}

// ListTasksForFlowMetrics mirrors the SQL: projectID's tasks gated by
// ownerUserID's project membership and bounded by since/until on
// created_at, each joined to its earliestMergeRequestForTask.
func (f *FakeQuerier) ListTasksForFlowMetrics(_ context.Context, arg db.ListTasksForFlowMetricsParams) ([]db.ListTasksForFlowMetricsRow, error) {
	if !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return []db.ListTasksForFlowMetricsRow{}, nil
	}
	items := []db.ListTasksForFlowMetricsRow{}
	for _, t := range f.tasks {
		if t.ProjectID != arg.ProjectID {
			continue
		}
		if arg.Since.Valid && t.CreatedAt.Time.Before(arg.Since.Time) {
			continue
		}
		if arg.Until.Valid && t.CreatedAt.Time.After(arg.Until.Time) {
			continue
		}
		row := db.ListTasksForFlowMetricsRow{
			ID:                      t.ID,
			CreatedAt:               t.CreatedAt,
			DesignStartedAt:         t.DesignStartedAt,
			ImplementationStartedAt: t.ImplementationStartedAt,
		}
		if mr, ok := f.earliestMergeRequestForTask(t.ID); ok {
			row.MrGitlabCreatedAt = mr.GitlabCreatedAt
			row.MrMergedAt = mr.MergedAt
		}
		items = append(items, row)
	}
	return items, nil
}

// ListTaskProgressEventsForFlowMetrics mirrors the SQL: every
// task_progress_events row for tasks ListTasksForFlowMetrics would select
// (same project-membership scoping and since/until bounding), ordered by
// task then occurred_at.
func (f *FakeQuerier) ListTaskProgressEventsForFlowMetrics(_ context.Context, arg db.ListTaskProgressEventsForFlowMetricsParams) ([]db.ListTaskProgressEventsForFlowMetricsRow, error) {
	if !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return []db.ListTaskProgressEventsForFlowMetricsRow{}, nil
	}
	inRange := map[uuid.UUID]bool{}
	for _, t := range f.tasks {
		if t.ProjectID != arg.ProjectID {
			continue
		}
		if arg.Since.Valid && t.CreatedAt.Time.Before(arg.Since.Time) {
			continue
		}
		if arg.Until.Valid && t.CreatedAt.Time.After(arg.Until.Time) {
			continue
		}
		inRange[t.ID] = true
	}
	items := []db.ListTaskProgressEventsForFlowMetricsRow{}
	for _, e := range f.taskProgressEvents {
		if !inRange[e.TaskID] {
			continue
		}
		items = append(items, db.ListTaskProgressEventsForFlowMetricsRow{
			TaskID:       e.TaskID,
			FromProgress: e.FromProgress,
			ToProgress:   e.ToProgress,
			OccurredAt:   e.OccurredAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TaskID != items[j].TaskID {
			return items[i].TaskID.String() < items[j].TaskID.String()
		}
		return items[i].OccurredAt.Time.Before(items[j].OccurredAt.Time)
	})
	return items, nil
}

// backlogsInFlowMetricsRange returns the IDs of projectID's backlogs that
// ownerUserID can see and that fall inside since/until on created_at — the
// same set ListBacklogsForFlowMetrics, ListBacklogProgressEventsForFlowMetrics
// and ListBacklogTaskCreatedAtForFlowMetrics all scope to.
func (f *FakeQuerier) backlogsInFlowMetricsRange(arg struct {
	ProjectID   uuid.UUID
	OwnerUserID uuid.UUID
	Since       pgtype.Timestamptz
	Until       pgtype.Timestamptz
}) map[uuid.UUID]bool {
	inRange := map[uuid.UUID]bool{}
	if !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return inRange
	}
	for _, b := range f.backlogs {
		if b.ProjectID != arg.ProjectID {
			continue
		}
		if arg.Since.Valid && b.CreatedAt.Time.Before(arg.Since.Time) {
			continue
		}
		if arg.Until.Valid && b.CreatedAt.Time.After(arg.Until.Time) {
			continue
		}
		inRange[b.ID] = true
	}
	return inRange
}

// ListBacklogsForFlowMetrics mirrors the SQL: projectID's backlogs gated by
// ownerUserID's project membership and bounded by since/until on
// backlogs.created_at.
func (f *FakeQuerier) ListBacklogsForFlowMetrics(_ context.Context, arg db.ListBacklogsForFlowMetricsParams) ([]db.ListBacklogsForFlowMetricsRow, error) {
	ids := f.backlogsInFlowMetricsRange(struct {
		ProjectID   uuid.UUID
		OwnerUserID uuid.UUID
		Since       pgtype.Timestamptz
		Until       pgtype.Timestamptz
	}{arg.ProjectID, arg.OwnerUserID, arg.Since, arg.Until})
	items := []db.ListBacklogsForFlowMetricsRow{}
	for _, b := range f.backlogs {
		if !ids[b.ID] {
			continue
		}
		items = append(items, db.ListBacklogsForFlowMetricsRow{ID: b.ID, CreatedAt: b.CreatedAt})
	}
	return items, nil
}

// ListBacklogProgressEventsForFlowMetrics mirrors the SQL: every
// backlog_progress_events row for backlogs ListBacklogsForFlowMetrics would
// select, ordered by backlog then occurred_at.
func (f *FakeQuerier) ListBacklogProgressEventsForFlowMetrics(_ context.Context, arg db.ListBacklogProgressEventsForFlowMetricsParams) ([]db.ListBacklogProgressEventsForFlowMetricsRow, error) {
	ids := f.backlogsInFlowMetricsRange(struct {
		ProjectID   uuid.UUID
		OwnerUserID uuid.UUID
		Since       pgtype.Timestamptz
		Until       pgtype.Timestamptz
	}{arg.ProjectID, arg.OwnerUserID, arg.Since, arg.Until})
	items := []db.ListBacklogProgressEventsForFlowMetricsRow{}
	for _, e := range f.backlogProgressEvents {
		if !ids[e.BacklogID] {
			continue
		}
		items = append(items, db.ListBacklogProgressEventsForFlowMetricsRow{
			BacklogID:    e.BacklogID,
			FromProgress: e.FromProgress,
			ToProgress:   e.ToProgress,
			OccurredAt:   e.OccurredAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].BacklogID != items[j].BacklogID {
			return items[i].BacklogID.String() < items[j].BacklogID.String()
		}
		return items[i].OccurredAt.Time.Before(items[j].OccurredAt.Time)
	})
	return items, nil
}

// ListBacklogTaskCreatedAtForFlowMetrics mirrors the SQL: every task filed
// under a backlog ListBacklogsForFlowMetrics would select, unfiltered by
// the task's own created_at (see the query's own doc comment for why).
func (f *FakeQuerier) ListBacklogTaskCreatedAtForFlowMetrics(_ context.Context, arg db.ListBacklogTaskCreatedAtForFlowMetricsParams) ([]db.ListBacklogTaskCreatedAtForFlowMetricsRow, error) {
	ids := f.backlogsInFlowMetricsRange(struct {
		ProjectID   uuid.UUID
		OwnerUserID uuid.UUID
		Since       pgtype.Timestamptz
		Until       pgtype.Timestamptz
	}{arg.ProjectID, arg.OwnerUserID, arg.Since, arg.Until})
	items := []db.ListBacklogTaskCreatedAtForFlowMetricsRow{}
	for _, t := range f.tasks {
		if !t.BacklogID.Valid || !ids[t.BacklogID.Bytes] {
			continue
		}
		items = append(items, db.ListBacklogTaskCreatedAtForFlowMetricsRow{
			BacklogID: t.BacklogID,
			CreatedAt: t.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].BacklogID.Bytes != items[j].BacklogID.Bytes {
			return items[i].BacklogID.String() < items[j].BacklogID.String()
		}
		return items[i].CreatedAt.Time.Before(items[j].CreatedAt.Time)
	})
	return items, nil
}

// dueOnLess reports whether a's due_on sorts before b's, matching due_on
// ASC's default NULLS LAST behaviour: a task with no due date always sorts
// after one that has one.
func dueOnLess(a, b pgtype.Date) bool {
	if a.Valid != b.Valid {
		return a.Valid
	}
	if !a.Valid {
		return false
	}
	return a.Time.Before(b.Time)
}

func dueOnEqual(a, b pgtype.Date) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Time.Equal(b.Time)
}

// ListTasksForOwner mirrors the SQL: every task across every project
// ownerUserID owns (through taskOwner, the same join every other owner-scoped
// task query uses), filtered by status/priority/due-before/due-after/
// started-before/project_ids — each following the "empty/NULL disables it"
// convention — then sorted and capped. See the real query's doc comment
// (internal/database/queries/tasks.sql) for why the ORDER BY tiers apply
// unconditionally rather than branching per sort value.
func (f *FakeQuerier) ListTasksForMember(_ context.Context, arg db.ListTasksForMemberParams) ([]db.ListTasksForMemberRow, error) {
	allowed := map[uuid.UUID]bool{}
	for _, id := range arg.ProjectIds {
		allowed[id] = true
	}

	items := []db.ListTasksForMemberRow{}
	for _, t := range f.tasks {
		if !f.hasMembership(t.ProjectID, arg.OwnerUserID) {
			continue
		}
		if arg.Status != "" && t.Status != arg.Status {
			continue
		}
		if arg.Priority != "" && t.Priority != arg.Priority {
			continue
		}
		if arg.Progress != "" && t.Progress != arg.Progress {
			continue
		}
		if arg.DueBefore.Valid && (!t.DueOn.Valid || t.DueOn.Time.After(arg.DueBefore.Time)) {
			continue
		}
		if arg.DueAfter.Valid && (!t.DueOn.Valid || t.DueOn.Time.Before(arg.DueAfter.Time)) {
			continue
		}
		if arg.StartedBefore.Valid && (!t.StartDate.Valid || t.StartDate.Time.After(arg.StartedBefore.Time)) {
			continue
		}
		if len(allowed) > 0 && !allowed[t.ProjectID] {
			continue
		}
		if arg.AssigneeMe && !f.matchesAssigneeMe(t.AssigneeGitlabUserID, arg.OwnerUserID, t.ProjectID) {
			continue
		}
		if !matchesTaskQuery(t.Title, t.Description, arg.Q) {
			continue
		}
		items = append(items, db.ListTasksForMemberRow{
			ID:                     t.ID,
			ProjectID:              t.ProjectID,
			BacklogID:              t.BacklogID,
			Title:                  t.Title,
			Description:            t.Description,
			Status:                 t.Status,
			ClosedAt:               t.ClosedAt,
			AssigneeGitlabUserID:   t.AssigneeGitlabUserID,
			AssigneeGitlabUsername: t.AssigneeGitlabUsername,
			Labels:                 t.Labels,
			DueOn:                  t.DueOn,
			StartDate:              t.StartDate,
			Priority:               t.Priority,
			Progress:               t.Progress,
			Position:               t.Position,
			CreatedByUserID:        t.CreatedByUserID,
			CreatedAt:              t.CreatedAt,
			UpdatedAt:              t.UpdatedAt,
			ProjectName:            f.projectsByID[t.ProjectID].Name,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		switch arg.Sort {
		case "priority":
			if ri, rj := priorityRank(items[i].Priority), priorityRank(items[j].Priority); ri != rj {
				return ri > rj
			}
		case "progress":
			if ri, rj := progressRank(items[i].Progress), progressRank(items[j].Progress); ri != rj {
				return ri < rj
			}
		case "updatedAt":
			if !items[i].UpdatedAt.Time.Equal(items[j].UpdatedAt.Time) {
				return items[i].UpdatedAt.Time.After(items[j].UpdatedAt.Time)
			}
		}
		if !dueOnEqual(items[i].DueOn, items[j].DueOn) {
			return dueOnLess(items[i].DueOn, items[j].DueOn)
		}
		if items[i].Position != items[j].Position {
			return items[i].Position < items[j].Position
		}
		return items[i].CreatedAt.Time.Before(items[j].CreatedAt.Time)
	})

	if int(arg.LimitCount) < len(items) {
		items = items[:arg.LimitCount]
	}
	return items, nil
}

// GetTaskForOwner mirrors the SQL: viewer-minimum (any project_members
// role) on the task's project.
func (f *FakeQuerier) GetTaskForOwner(_ context.Context, arg db.GetTaskForOwnerParams) (db.Task, error) {
	t, ok := f.tasksByID[arg.ID]
	if !ok || !f.hasMembership(t.ProjectID, arg.OwnerUserID) {
		return db.Task{}, pgx.ErrNoRows
	}
	return t, nil
}

// GetTaskForProject mirrors the SQL: scoped by project_id directly, with no
// owner join, for the AI-facing bearer-token path.
func (f *FakeQuerier) GetTaskForProject(_ context.Context, arg db.GetTaskForProjectParams) (db.Task, error) {
	t, ok := f.tasksByID[arg.ID]
	if !ok || t.ProjectID != arg.ProjectID {
		return db.Task{}, pgx.ErrNoRows
	}
	return t, nil
}

// GetTaskProjectID mirrors the SQL: no owner join, just the task's own
// project_id.
func (f *FakeQuerier) GetTaskProjectID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	t, ok := f.tasksByID[id]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return t.ProjectID, nil
}

// CountFailedSyncTasksByProjectForOwner mirrors the SQL: a task counts as
// failed the same way internal/task derives a single task's sync_status,
// from task_gitlab_links when a link exists or from its most recent sync_jobs
// row (necessarily issue.create) when it doesn't.
func (f *FakeQuerier) CountFailedSyncTasksByProjectForOwner(_ context.Context, arg db.CountFailedSyncTasksByProjectForOwnerParams) (int64, error) {
	if !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return 0, nil
	}
	return f.countFailedSyncTasks(arg.ProjectID), nil
}

// countFailedSyncTasks counts projectID's failed-sync tasks the same way the
// real SQL does, shared by CountFailedSyncTasksByProjectForOwner and
// ListFailedSyncProjectsByOwner.
func (f *FakeQuerier) countFailedSyncTasks(projectID uuid.UUID) int64 {
	var count int64
	for _, t := range f.tasks {
		if t.ProjectID != projectID {
			continue
		}
		if link, ok := f.taskGitlabLinksByTaskID[t.ID]; ok {
			if link.SyncStatus == "failed" {
				count++
			}
			continue
		}
		for i := len(f.syncJobs) - 1; i >= 0; i-- {
			j := f.syncJobs[i]
			if j.TaskID.Valid && j.TaskID.Bytes == t.ID && j.Status == "failed" {
				count++
				break
			}
		}
	}
	return count
}

// ListFailedSyncProjectsByMember mirrors the SQL: every project userID is a
// member of (any role) with a non-zero failed-sync task count, ordered by
// updated_at DESC.
func (f *FakeQuerier) ListFailedSyncProjectsByMember(_ context.Context, userID uuid.UUID) ([]db.ListFailedSyncProjectsByMemberRow, error) {
	items := []db.ListFailedSyncProjectsByMemberRow{}
	for _, p := range f.projects {
		if !f.hasMembership(p.ID, userID) {
			continue
		}
		count := f.countFailedSyncTasks(p.ID)
		if count == 0 {
			continue
		}
		items = append(items, db.ListFailedSyncProjectsByMemberRow{
			ID:                  p.ID,
			OwnerUserID:         p.OwnerUserID,
			Name:                p.Name,
			Description:         p.Description,
			CreatedAt:           p.CreatedAt,
			UpdatedAt:           p.UpdatedAt,
			FailedSyncTaskCount: count,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.Time.After(items[j].UpdatedAt.Time)
	})
	return items, nil
}

func (f *FakeQuerier) UpdateTaskForOwner(_ context.Context, arg db.UpdateTaskForOwnerParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	if !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Task{}, pgx.ErrNoRows
	}

	existing.BacklogID = arg.BacklogID
	existing.Title = arg.Title
	existing.Description = arg.Description
	existing.AssigneeGitlabUserID = arg.AssigneeGitlabUserID
	existing.AssigneeGitlabUsername = arg.AssigneeGitlabUsername
	existing.Labels = arg.Labels
	existing.DueOn = arg.DueOn
	existing.StartDate = arg.StartDate
	existing.Priority = arg.Priority
	existing.Progress = arg.Progress
	existing.Position = arg.Position
	existing.UpdatedAt = now()

	f.storeTask(existing)
	return existing, nil
}

func (f *FakeQuerier) AssignTaskBacklogForOwner(_ context.Context, arg db.AssignTaskBacklogForOwnerParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	if !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.BacklogID = arg.BacklogID
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

// ReorderTasks mirrors the SQL: resequences position 0..n-1 for every task
// in arg.TaskIds belonging to arg.ProjectID and matching arg.BacklogID (nil
// backlog_id matches Unclassified tasks, an IsNotDistinctFrom-style
// comparison), in the order given. A task ID outside that project/backlog
// bucket is silently skipped, matching the real query's WHERE clause —
// internal/task.Service.Reorder is what guarantees the set matches before
// calling this.
func (f *FakeQuerier) ReorderTasks(_ context.Context, arg db.ReorderTasksParams) error {
	for i, id := range arg.TaskIds {
		t, ok := f.tasksByID[id]
		if !ok || t.ProjectID != arg.ProjectID {
			continue
		}
		if t.BacklogID.Valid != arg.BacklogID.Valid || (t.BacklogID.Valid && t.BacklogID.Bytes != arg.BacklogID.Bytes) {
			continue
		}
		t.Position = int32(i)
		t.UpdatedAt = now()
		f.storeTask(t)
	}
	return nil
}

func (f *FakeQuerier) CloseTaskForOwner(_ context.Context, arg db.CloseTaskForOwnerParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	if !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.Status = "closed"
	existing.ClosedAt = now()
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

func (f *FakeQuerier) ReopenTaskForOwner(_ context.Context, arg db.ReopenTaskForOwnerParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	if !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.Status = "open"
	existing.ClosedAt = pgtype.Timestamptz{}
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

func (f *FakeQuerier) MarkTaskDesignStarted(_ context.Context, arg db.MarkTaskDesignStartedParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	if !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.DesignStartedAt = now()
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

func (f *FakeQuerier) MarkTaskImplementationStarted(_ context.Context, arg db.MarkTaskImplementationStartedParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	if !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "member") {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.ImplementationStartedAt = now()
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

// DeleteTaskForOwner returns the number of rows affected, so callers can
// tell "deleted" from "not yours / not there" exactly as Postgres does.
func (f *FakeQuerier) DeleteTaskForOwner(_ context.Context, arg db.DeleteTaskForOwnerParams) (int64, error) {
	t, ok := f.tasksByID[arg.ID]
	if !ok {
		return 0, nil
	}
	if !f.hasRoleAtLeast(t.ProjectID, arg.OwnerUserID, "member") {
		return 0, nil
	}
	delete(f.tasksByID, t.ID)
	for i, x := range f.tasks {
		if x.ID == t.ID {
			f.tasks = append(f.tasks[:i], f.tasks[i+1:]...)
			break
		}
	}
	return 1, nil
}

// UpsertTaskAIContext mirrors the SQL's ON CONFLICT DO UPDATE: the first
// call creates the row, later calls overwrite it and bump updated_at.
func (f *FakeQuerier) UpsertTaskAIContext(_ context.Context, arg db.UpsertTaskAIContextParams) (db.TaskAiContext, error) {
	c := db.TaskAiContext{
		TaskID:             arg.TaskID,
		AcceptanceCriteria: arg.AcceptanceCriteria,
		AiContext:          arg.AiContext,
		AllowedScope:       arg.AllowedScope,
		ForbiddenScope:     arg.ForbiddenScope,
		UpdatedAt:          now(),
	}
	f.taskAIContextsByTaskID[arg.TaskID] = c
	return c, nil
}

func (f *FakeQuerier) GetTaskAIContext(_ context.Context, taskID uuid.UUID) (db.TaskAiContext, error) {
	c, ok := f.taskAIContextsByTaskID[taskID]
	if !ok {
		return db.TaskAiContext{}, pgx.ErrNoRows
	}
	return c, nil
}

// SeedTaskDependency inserts a ready-made predecessor->successor dependency
// directly, bypassing cycle validation. Returns the stored row.
func (f *FakeQuerier) SeedTaskDependency(predecessorTaskID, successorTaskID uuid.UUID) db.TaskDependency {
	d := db.TaskDependency{
		ID:                uuid.New(),
		PredecessorTaskID: predecessorTaskID,
		SuccessorTaskID:   successorTaskID,
		CreatedAt:         now(),
	}
	f.taskDependenciesByID[d.ID] = d
	f.taskDependencies = append(f.taskDependencies, d)
	return d
}

func (f *FakeQuerier) CreateTaskDependency(_ context.Context, arg db.CreateTaskDependencyParams) (db.TaskDependency, error) {
	return f.SeedTaskDependency(arg.PredecessorTaskID, arg.SuccessorTaskID), nil
}

// GetTaskDependencyProjectID mirrors the SQL: resolved through the
// predecessor task's project_id, the same way DeleteTaskDependencyForOwner
// resolves ownership.
func (f *FakeQuerier) GetTaskDependencyProjectID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	d, ok := f.taskDependenciesByID[id]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	t, ok := f.tasksByID[d.PredecessorTaskID]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return t.ProjectID, nil
}

// ListTaskDependenciesByProject mirrors the SQL: every dependency whose
// predecessor task belongs to projectID (both tasks in a dependency always
// belong to the same project, enforced by internal/taskdependency).
func (f *FakeQuerier) ListTaskDependenciesByProject(_ context.Context, projectID uuid.UUID) ([]db.TaskDependency, error) {
	items := []db.TaskDependency{}
	for _, d := range f.taskDependencies {
		t, ok := f.tasksByID[d.PredecessorTaskID]
		if !ok || t.ProjectID != projectID {
			continue
		}
		items = append(items, d)
	}
	return items, nil
}

// DeleteTaskDependencyForOwner mirrors the SQL: ownership is checked through
// the predecessor task's project, the same way DeleteTaskForOwner checks a
// task's own project.
func (f *FakeQuerier) DeleteTaskDependencyForOwner(_ context.Context, arg db.DeleteTaskDependencyForOwnerParams) (int64, error) {
	d, ok := f.taskDependenciesByID[arg.ID]
	if !ok {
		return 0, nil
	}
	t, ok := f.tasksByID[d.PredecessorTaskID]
	if !ok {
		return 0, nil
	}
	if !f.hasRoleAtLeast(t.ProjectID, arg.OwnerUserID, "member") {
		return 0, nil
	}
	delete(f.taskDependenciesByID, d.ID)
	for i, x := range f.taskDependencies {
		if x.ID == d.ID {
			f.taskDependencies = append(f.taskDependencies[:i], f.taskDependencies[i+1:]...)
			break
		}
	}
	return 1, nil
}

// CreateTaskComment mirrors the SQL: exactly one of AuthorUserID/
// AuthorTokenID is expected to be set by the caller (internal/taskcomment).
func (f *FakeQuerier) CreateTaskComment(_ context.Context, arg db.CreateTaskCommentParams) (db.TaskComment, error) {
	c := db.TaskComment{
		ID:            uuid.New(),
		TaskID:        arg.TaskID,
		AuthorUserID:  arg.AuthorUserID,
		AuthorTokenID: arg.AuthorTokenID,
		AuthorKind:    arg.AuthorKind,
		Body:          arg.Body,
		CreatedAt:     now(),
		UpdatedAt:     now(),
	}
	f.taskCommentsByID[c.ID] = c
	f.taskComments = append(f.taskComments, c)
	return c, nil
}

// ListTaskCommentsByTask mirrors the SQL: every comment on taskID, oldest
// first (insertion order).
func (f *FakeQuerier) ListTaskCommentsByTask(_ context.Context, taskID uuid.UUID) ([]db.TaskComment, error) {
	items := []db.TaskComment{}
	for _, c := range f.taskComments {
		if c.TaskID == taskID {
			items = append(items, c)
		}
	}
	return items, nil
}

func (f *FakeQuerier) GetTaskCommentByID(_ context.Context, id uuid.UUID) (db.TaskComment, error) {
	c, ok := f.taskCommentsByID[id]
	if !ok {
		return db.TaskComment{}, pgx.ErrNoRows
	}
	return c, nil
}

// CreateTaskProgressEvent mirrors the SQL: an append-only row, never
// updated or looked up by ID (internal/task.Service.Update is the only
// writer).
func (f *FakeQuerier) CreateTaskProgressEvent(_ context.Context, arg db.CreateTaskProgressEventParams) (db.TaskProgressEvent, error) {
	e := db.TaskProgressEvent{
		ID:           uuid.New(),
		TaskID:       arg.TaskID,
		FromProgress: arg.FromProgress,
		ToProgress:   arg.ToProgress,
		ActorKind:    arg.ActorKind,
		ActorUserID:  arg.ActorUserID,
		OccurredAt:   now(),
	}
	f.taskProgressEvents = append(f.taskProgressEvents, e)
	return e, nil
}

// ListTaskProgressEventsByTask mirrors the SQL: every event on taskID,
// oldest first (insertion order).
func (f *FakeQuerier) ListTaskProgressEventsByTask(_ context.Context, taskID uuid.UUID) ([]db.TaskProgressEvent, error) {
	items := []db.TaskProgressEvent{}
	for _, e := range f.taskProgressEvents {
		if e.TaskID == taskID {
			items = append(items, e)
		}
	}
	return items, nil
}

// SeedTaskProgressEvent inserts a ready-made progress-transition event
// directly, bypassing internal/task.Service.Update, so a test (e.g.
// internal/flowmetrics) can control occurred_at precisely instead of
// getting CreateTaskProgressEvent's now().
func (f *FakeQuerier) SeedTaskProgressEvent(taskID uuid.UUID, fromProgress, toProgress string, occurredAt time.Time) db.TaskProgressEvent {
	e := db.TaskProgressEvent{
		ID:           uuid.New(),
		TaskID:       taskID,
		FromProgress: fromProgress,
		ToProgress:   toProgress,
		ActorKind:    "agent",
		OccurredAt:   pgtype.Timestamptz{Time: occurredAt, Valid: true},
	}
	f.taskProgressEvents = append(f.taskProgressEvents, e)
	return e
}

// CreateBacklogProgressEvent mirrors the SQL: an append-only row, never
// updated or looked up by ID (internal/backlog.Service.Update is the only
// writer).
func (f *FakeQuerier) CreateBacklogProgressEvent(_ context.Context, arg db.CreateBacklogProgressEventParams) (db.BacklogProgressEvent, error) {
	e := db.BacklogProgressEvent{
		ID:           uuid.New(),
		BacklogID:    arg.BacklogID,
		FromProgress: arg.FromProgress,
		ToProgress:   arg.ToProgress,
		ActorKind:    arg.ActorKind,
		ActorUserID:  arg.ActorUserID,
		OccurredAt:   now(),
	}
	f.backlogProgressEvents = append(f.backlogProgressEvents, e)
	return e, nil
}

// ListBacklogProgressEventsByBacklog mirrors the SQL: every event on
// backlogID, oldest first (insertion order).
func (f *FakeQuerier) ListBacklogProgressEventsByBacklog(_ context.Context, backlogID uuid.UUID) ([]db.BacklogProgressEvent, error) {
	items := []db.BacklogProgressEvent{}
	for _, e := range f.backlogProgressEvents {
		if e.BacklogID == backlogID {
			items = append(items, e)
		}
	}
	return items, nil
}

// SeedBacklogProgressEvent inserts a ready-made progress-transition event
// directly, bypassing internal/backlog.Service.Update, so a test (e.g.
// internal/flowmetrics) can control occurred_at precisely instead of
// getting CreateBacklogProgressEvent's now().
func (f *FakeQuerier) SeedBacklogProgressEvent(backlogID uuid.UUID, fromProgress, toProgress string, occurredAt time.Time) db.BacklogProgressEvent {
	e := db.BacklogProgressEvent{
		ID:           uuid.New(),
		BacklogID:    backlogID,
		FromProgress: fromProgress,
		ToProgress:   toProgress,
		ActorKind:    "agent",
		OccurredAt:   pgtype.Timestamptz{Time: occurredAt, Valid: true},
	}
	f.backlogProgressEvents = append(f.backlogProgressEvents, e)
	return e
}

// CreateGitlabTaskComment mirrors the SQL: always author_kind 'gitlab' with
// gitlab_note_id set, for a comment mirrored in from a webhook (#104).
func (f *FakeQuerier) CreateGitlabTaskComment(_ context.Context, arg db.CreateGitlabTaskCommentParams) (db.TaskComment, error) {
	c := db.TaskComment{
		ID:           uuid.New(),
		TaskID:       arg.TaskID,
		AuthorKind:   "gitlab",
		Body:         arg.Body,
		GitlabNoteID: arg.GitlabNoteID,
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}
	f.taskCommentsByID[c.ID] = c
	f.taskComments = append(f.taskComments, c)
	return c, nil
}

// GetTaskCommentByGitlabNoteID mirrors the SQL: used to detect an echo of a
// comment FlowLens itself pushed to GitLab (#104).
func (f *FakeQuerier) GetTaskCommentByGitlabNoteID(_ context.Context, gitlabNoteID pgtype.Int8) (db.TaskComment, error) {
	for _, c := range f.taskComments {
		if c.GitlabNoteID.Valid && gitlabNoteID.Valid && c.GitlabNoteID.Int64 == gitlabNoteID.Int64 {
			return c, nil
		}
	}
	return db.TaskComment{}, pgx.ErrNoRows
}

// SetTaskCommentGitlabNoteID mirrors the SQL: records the GitLab note id
// returned by a successful push (#104).
func (f *FakeQuerier) SetTaskCommentGitlabNoteID(_ context.Context, arg db.SetTaskCommentGitlabNoteIDParams) error {
	c, ok := f.taskCommentsByID[arg.ID]
	if !ok {
		return pgx.ErrNoRows
	}
	c.GitlabNoteID = arg.GitlabNoteID
	c.UpdatedAt = now()
	f.taskCommentsByID[arg.ID] = c
	for i, x := range f.taskComments {
		if x.ID == arg.ID {
			f.taskComments[i] = c
			break
		}
	}
	return nil
}

// GetTaskCommentProjectID mirrors the SQL: resolved through the comment's
// task, the same way GetTaskDependencyProjectID resolves through a
// dependency's predecessor task.
func (f *FakeQuerier) GetTaskCommentProjectID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	c, ok := f.taskCommentsByID[id]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	t, ok := f.tasksByID[c.TaskID]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return t.ProjectID, nil
}

func (f *FakeQuerier) DeleteTaskCommentByID(_ context.Context, id uuid.UUID) (int64, error) {
	if _, ok := f.taskCommentsByID[id]; !ok {
		return 0, nil
	}
	delete(f.taskCommentsByID, id)
	for i, c := range f.taskComments {
		if c.ID == id {
			f.taskComments = append(f.taskComments[:i], f.taskComments[i+1:]...)
			break
		}
	}
	return 1, nil
}

// UpsertGitlabConnection mirrors the SQL's ON CONFLICT (project_id) DO
// UPDATE: the first call creates the row, later calls overwrite it and
// reset the verification fields to a fresh success.
func (f *FakeQuerier) UpsertGitlabConnection(_ context.Context, arg db.UpsertGitlabConnectionParams) (db.GitlabConnection, error) {
	existing, ok := f.gitlabConnectionsByProjectID[arg.ProjectID]
	c := db.GitlabConnection{
		ID:                  uuid.New(),
		ProjectID:           arg.ProjectID,
		BaseUrl:             arg.BaseUrl,
		EncryptedToken:      arg.EncryptedToken,
		TokenGitlabUserID:   arg.TokenGitlabUserID,
		TokenGitlabUsername: arg.TokenGitlabUsername,
		LastVerifiedAt:      now(),
		LastVerifyError:     "",
		CreatedAt:           now(),
		UpdatedAt:           now(),
	}
	if ok {
		c.ID = existing.ID
		c.CreatedAt = existing.CreatedAt
	}
	f.gitlabConnectionsByProjectID[arg.ProjectID] = c
	f.gitlabConnectionsByID[c.ID] = c
	return c, nil
}

// GetGitlabConnectionForOwner mirrors the SQL: a connection whose project
// belongs to someone else is reported as missing, never as a distinct
// "forbidden" outcome.
func (f *FakeQuerier) GetGitlabConnectionForOwner(_ context.Context, arg db.GetGitlabConnectionForOwnerParams) (db.GitlabConnection, error) {
	c, ok := f.gitlabConnectionsByProjectID[arg.ProjectID]
	if !ok || !f.hasMembership(c.ProjectID, arg.OwnerUserID) {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	return c, nil
}

// GetGitlabConnectionByIDForOwner mirrors the SQL: same lookup as
// GetGitlabConnectionForOwner, but keyed by the connection's own ID.
func (f *FakeQuerier) GetGitlabConnectionByIDForOwner(_ context.Context, arg db.GetGitlabConnectionByIDForOwnerParams) (db.GitlabConnection, error) {
	c, ok := f.gitlabConnectionsByID[arg.ID]
	if !ok || !f.hasMembership(c.ProjectID, arg.OwnerUserID) {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	return c, nil
}

// GetGitlabConnectionByID is unscoped, mirroring the SQL used by the outbox
// worker (internal/issuesync), which has no acting user to check against.
func (f *FakeQuerier) GetGitlabConnectionByID(_ context.Context, id uuid.UUID) (db.GitlabConnection, error) {
	c, ok := f.gitlabConnectionsByID[id]
	if !ok {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	return c, nil
}

func (f *FakeQuerier) UpdateGitlabConnectionVerificationForOwner(_ context.Context, arg db.UpdateGitlabConnectionVerificationForOwnerParams) (db.GitlabConnection, error) {
	existing, ok := f.gitlabConnectionsByProjectID[arg.ProjectID]
	if !ok || !f.hasRoleAtLeast(existing.ProjectID, arg.OwnerUserID, "owner") {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}

	existing.TokenGitlabUserID = arg.TokenGitlabUserID
	existing.TokenGitlabUsername = arg.TokenGitlabUsername
	existing.LastVerifiedAt = arg.LastVerifiedAt
	existing.LastVerifyError = arg.LastVerifyError
	existing.UpdatedAt = now()

	f.gitlabConnectionsByProjectID[arg.ProjectID] = existing
	f.gitlabConnectionsByID[existing.ID] = existing
	return existing, nil
}

// DeleteGitlabConnectionForOwner returns the number of rows affected, so
// callers can tell "deleted" from "not yours / not there" exactly as
// Postgres does.
func (f *FakeQuerier) DeleteGitlabConnectionForOwner(_ context.Context, arg db.DeleteGitlabConnectionForOwnerParams) (int64, error) {
	c, ok := f.gitlabConnectionsByProjectID[arg.ProjectID]
	if !ok || !f.hasRoleAtLeast(c.ProjectID, arg.OwnerUserID, "owner") {
		return 0, nil
	}
	delete(f.gitlabConnectionsByProjectID, arg.ProjectID)
	delete(f.gitlabConnectionsByID, c.ID)
	return 1, nil
}

// SeedGitlabConnection inserts a ready-made GitLab connection directly,
// bypassing verification. encryptedToken must be a value a caller's Cipher
// can decrypt, since services that read it back through gitlabconn.Dial
// decrypt it. Use it in tests that need a pre-existing connection to link
// GitLab projects against. Returns the stored row.
func (f *FakeQuerier) SeedGitlabConnection(projectID uuid.UUID, encryptedToken []byte) db.GitlabConnection {
	c := db.GitlabConnection{
		ID:             uuid.New(),
		ProjectID:      projectID,
		BaseUrl:        "https://gitlab.example.com",
		EncryptedToken: encryptedToken,
		CreatedAt:      now(),
		UpdatedAt:      now(),
	}
	f.gitlabConnectionsByProjectID[projectID] = c
	f.gitlabConnectionsByID[c.ID] = c
	return c
}

// matchesAssigneeMe mirrors the SQL's gitlab_connections/user_gitlab_identities
// join for ?assignee=me (issue #102): userID's registered identity for
// projectID's own GitLab connection base URL must match assigneeGitlabUserID
// exactly. A project with no connection, or a user with no registered
// identity for that connection's base URL, never matches.
func (f *FakeQuerier) matchesAssigneeMe(assigneeGitlabUserID pgtype.Int8, userID, projectID uuid.UUID) bool {
	if !assigneeGitlabUserID.Valid {
		return false
	}
	conn, ok := f.gitlabConnectionsByProjectID[projectID]
	if !ok {
		return false
	}
	identity, ok := f.userGitlabIdentities[userGitlabIdentityKey{userID: userID, baseURL: conn.BaseUrl}]
	if !ok {
		return false
	}
	return identity.GitlabUserID == assigneeGitlabUserID.Int64
}

// UpsertUserGitlabIdentity mirrors the SQL's ON CONFLICT (user_id,
// gitlab_base_url) DO UPDATE.
func (f *FakeQuerier) UpsertUserGitlabIdentity(_ context.Context, arg db.UpsertUserGitlabIdentityParams) (db.UserGitlabIdentity, error) {
	key := userGitlabIdentityKey{userID: arg.UserID, baseURL: arg.GitlabBaseUrl}
	existing, ok := f.userGitlabIdentities[key]
	id := uuid.New()
	createdAt := now()
	if ok {
		id = existing.ID
		createdAt = existing.CreatedAt
	}
	i := db.UserGitlabIdentity{
		ID:             id,
		UserID:         arg.UserID,
		GitlabBaseUrl:  arg.GitlabBaseUrl,
		GitlabUserID:   arg.GitlabUserID,
		GitlabUsername: arg.GitlabUsername,
		CreatedAt:      createdAt,
		UpdatedAt:      now(),
	}
	f.userGitlabIdentities[key] = i
	return i, nil
}

// ListUserGitlabIdentitiesByUser mirrors the SQL: every identity userID has
// registered, ordered by base URL.
func (f *FakeQuerier) ListUserGitlabIdentitiesByUser(_ context.Context, userID uuid.UUID) ([]db.UserGitlabIdentity, error) {
	items := []db.UserGitlabIdentity{}
	for key, i := range f.userGitlabIdentities {
		if key.userID == userID {
			items = append(items, i)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GitlabBaseUrl < items[j].GitlabBaseUrl })
	return items, nil
}

// SeedUserGitlabIdentity registers a ready-made GitLab identity directly,
// bypassing gitlabidentity.Service, for tests that need one to already exist
// (e.g. an ?assignee=me filter test).
func (f *FakeQuerier) SeedUserGitlabIdentity(userID uuid.UUID, baseURL string, gitlabUserID int64, gitlabUsername string) db.UserGitlabIdentity {
	i, _ := f.UpsertUserGitlabIdentity(context.Background(), db.UpsertUserGitlabIdentityParams{
		UserID:         userID,
		GitlabBaseUrl:  baseURL,
		GitlabUserID:   gitlabUserID,
		GitlabUsername: gitlabUsername,
	})
	return i
}

// linkedProjectProjectID returns the project ID a linked GitLab project
// belongs to (through its connection), mirroring the JOIN chain the real
// queries perform.
func (f *FakeQuerier) linkedProjectProjectID(l db.LinkedGitlabProject) (uuid.UUID, bool) {
	conn, ok := f.gitlabConnectionsByID[l.GitlabConnectionID]
	if !ok {
		return uuid.Nil, false
	}
	if _, ok := f.projectsByID[conn.ProjectID]; !ok {
		return uuid.Nil, false
	}
	return conn.ProjectID, true
}

// storeLinkedGitlabProject inserts l if it is new, or overwrites the
// existing row in place (preserving its position in
// f.linkedGitlabProjects) otherwise.
func (f *FakeQuerier) storeLinkedGitlabProject(l db.LinkedGitlabProject) {
	f.linkedGitlabProjectsByID[l.ID] = l
	for i, x := range f.linkedGitlabProjects {
		if x.ID == l.ID {
			f.linkedGitlabProjects[i] = l
			return
		}
	}
	f.linkedGitlabProjects = append(f.linkedGitlabProjects, l)
}

// CreateLinkedGitlabProject mirrors the SQL: the first link created for a
// connection becomes its default, and a (gitlab_connection_id,
// gitlab_project_id) duplicate is a unique-constraint violation.
func (f *FakeQuerier) CreateLinkedGitlabProject(_ context.Context, arg db.CreateLinkedGitlabProjectParams) (db.LinkedGitlabProject, error) {
	isDefault := true
	for _, l := range f.linkedGitlabProjects {
		if l.GitlabConnectionID != arg.GitlabConnectionID {
			continue
		}
		if l.GitlabProjectID == arg.GitlabProjectID {
			return db.LinkedGitlabProject{}, &pgconn.PgError{Code: "23505", ConstraintName: "linked_gitlab_projects_gitlab_connection_id_gitlab_project_id_key"}
		}
		isDefault = false
	}

	l := db.LinkedGitlabProject{
		ID:                  uuid.New(),
		GitlabConnectionID:  arg.GitlabConnectionID,
		GitlabProjectID:     arg.GitlabProjectID,
		PathWithNamespace:   arg.PathWithNamespace,
		Name:                arg.Name,
		WebUrl:              arg.WebUrl,
		SyncScope:           arg.SyncScope,
		SyncLabels:          arg.SyncLabels,
		InitialImportStatus: "pending",
		IsDefault:           isDefault,
		CreatedAt:           now(),
		UpdatedAt:           now(),
	}
	f.storeLinkedGitlabProject(l)
	return l, nil
}

func (f *FakeQuerier) ListLinkedGitlabProjectsForOwner(_ context.Context, arg db.ListLinkedGitlabProjectsForOwnerParams) ([]db.LinkedGitlabProject, error) {
	items := []db.LinkedGitlabProject{}
	for _, l := range f.linkedGitlabProjects {
		conn, ok := f.gitlabConnectionsByID[l.GitlabConnectionID]
		if !ok || conn.ProjectID != arg.ProjectID {
			continue
		}
		if !f.hasMembership(conn.ProjectID, arg.OwnerUserID) {
			continue
		}
		items = append(items, l)
	}
	return items, nil
}

// GetLinkedGitlabProjectForOwner mirrors the SQL: viewer-minimum (any
// project_members role) on the link's project.
func (f *FakeQuerier) GetLinkedGitlabProjectForOwner(_ context.Context, arg db.GetLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	l, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(l)
	if !ok || !f.hasMembership(projectID, arg.OwnerUserID) {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	return l, nil
}

// GetLinkedGitlabProjectByID is unscoped, mirroring the SQL used by the
// outbox worker (internal/issuesync), which has no acting user to check
// against.
func (f *FakeQuerier) GetLinkedGitlabProjectByID(_ context.Context, id uuid.UUID) (db.LinkedGitlabProject, error) {
	l, ok := f.linkedGitlabProjectsByID[id]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	return l, nil
}

// GetLinkedGitlabProjectProjectID mirrors the SQL: resolved through the
// link's connection, the same way linkedProjectOwner is, since a linked
// project has no project_id column of its own.
func (f *FakeQuerier) GetLinkedGitlabProjectProjectID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	l, ok := f.linkedGitlabProjectsByID[id]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	conn, ok := f.gitlabConnectionsByID[l.GitlabConnectionID]
	if !ok {
		return uuid.Nil, pgx.ErrNoRows
	}
	return conn.ProjectID, nil
}

// GetLinkedGitlabProjectInProjectForOwner mirrors the SQL: the link, but only
// when it belongs to arg.ProjectID's own GitLab connection and the caller can
// see that project. Used by internal/backlog to validate a backlog's own issue
// destination.
func (f *FakeQuerier) GetLinkedGitlabProjectInProjectForOwner(_ context.Context, arg db.GetLinkedGitlabProjectInProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	l, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(l)
	if !ok || projectID != arg.ProjectID || !f.hasMembership(projectID, arg.OwnerUserID) {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	return l, nil
}

// GetBacklogLinkedGitlabProjectForOwner mirrors the SQL: the link a backlog
// names as its own issue destination, or no rows when it names none.
func (f *FakeQuerier) GetBacklogLinkedGitlabProjectForOwner(_ context.Context, arg db.GetBacklogLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	b, ok := f.backlogsByID[arg.ID]
	if !ok || !b.DefaultLinkedGitlabProjectID.Valid {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	if !f.hasMembership(b.ProjectID, arg.OwnerUserID) {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	l, ok := f.linkedGitlabProjectsByID[uuid.UUID(b.DefaultLinkedGitlabProjectID.Bytes)]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(l)
	if !ok || projectID != b.ProjectID {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	return l, nil
}

// GetDefaultLinkedGitlabProjectForOwner mirrors the SQL: the project's
// default link, scoped through its connection to the owning project like
// every other linked-project query.
func (f *FakeQuerier) GetDefaultLinkedGitlabProjectForOwner(_ context.Context, arg db.GetDefaultLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	for _, l := range f.linkedGitlabProjects {
		if !l.IsDefault {
			continue
		}
		conn, ok := f.gitlabConnectionsByID[l.GitlabConnectionID]
		if !ok || conn.ProjectID != arg.ProjectID {
			continue
		}
		if !f.hasMembership(conn.ProjectID, arg.OwnerUserID) {
			continue
		}
		return l, nil
	}
	return db.LinkedGitlabProject{}, pgx.ErrNoRows
}

func (f *FakeQuerier) UpdateLinkedGitlabProjectSyncScopeForOwner(_ context.Context, arg db.UpdateLinkedGitlabProjectSyncScopeForOwnerParams) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(existing)
	if !ok || !f.hasRoleAtLeast(projectID, arg.OwnerUserID, "member") {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	existing.SyncScope = arg.SyncScope
	existing.SyncLabels = arg.SyncLabels
	existing.UpdatedAt = now()
	f.storeLinkedGitlabProject(existing)
	return existing, nil
}

// ClearDefaultLinkedGitlabProjectsForOwner unsets is_default on every other
// link in the same connection as arg.ID, mirroring the SQL.
func (f *FakeQuerier) ClearDefaultLinkedGitlabProjectsForOwner(_ context.Context, arg db.ClearDefaultLinkedGitlabProjectsForOwnerParams) error {
	target, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return nil
	}
	projectID, ok := f.linkedProjectProjectID(target)
	if !ok || !f.hasRoleAtLeast(projectID, arg.OwnerUserID, "member") {
		return nil
	}
	for _, l := range f.linkedGitlabProjects {
		if l.ID != target.ID && l.GitlabConnectionID == target.GitlabConnectionID && l.IsDefault {
			l.IsDefault = false
			l.UpdatedAt = now()
			f.storeLinkedGitlabProject(l)
		}
	}
	return nil
}

func (f *FakeQuerier) SetDefaultLinkedGitlabProjectForOwner(_ context.Context, arg db.SetDefaultLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(existing)
	if !ok || !f.hasRoleAtLeast(projectID, arg.OwnerUserID, "member") {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	existing.IsDefault = true
	existing.UpdatedAt = now()
	f.storeLinkedGitlabProject(existing)
	return existing, nil
}

// DeleteLinkedGitlabProjectForOwner returns the deleted row, mirroring the
// SQL, so the caller can tell whether the removed link was the default one.
func (f *FakeQuerier) DeleteLinkedGitlabProjectForOwner(_ context.Context, arg db.DeleteLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(existing)
	if !ok || !f.hasRoleAtLeast(projectID, arg.OwnerUserID, "member") {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	delete(f.linkedGitlabProjectsByID, existing.ID)
	for i, x := range f.linkedGitlabProjects {
		if x.ID == existing.ID {
			f.linkedGitlabProjects = append(f.linkedGitlabProjects[:i], f.linkedGitlabProjects[i+1:]...)
			break
		}
	}
	f.clearBacklogDefaultLink(existing.ID)
	return existing, nil
}

// clearBacklogDefaultLink mirrors backlogs.default_linked_gitlab_project_id's
// ON DELETE SET NULL (migration 000021): unlinking a GitLab project falls
// every backlog that pointed at it back to the project's default link rather
// than deleting the backlog.
func (f *FakeQuerier) clearBacklogDefaultLink(linkID uuid.UUID) {
	for i, b := range f.backlogs {
		if b.DefaultLinkedGitlabProjectID.Valid && uuid.UUID(b.DefaultLinkedGitlabProjectID.Bytes) == linkID {
			f.backlogs[i].DefaultLinkedGitlabProjectID = pgtype.UUID{}
			f.backlogsByID[b.ID] = f.backlogs[i]
		}
	}
}

// PromoteOldestLinkedGitlabProjectAsDefault makes the oldest remaining link
// in gitlabConnectionID the new default. A no-op if none remain.
func (f *FakeQuerier) PromoteOldestLinkedGitlabProjectAsDefault(_ context.Context, gitlabConnectionID uuid.UUID) error {
	var oldest *db.LinkedGitlabProject
	for i := range f.linkedGitlabProjects {
		l := &f.linkedGitlabProjects[i]
		if l.GitlabConnectionID != gitlabConnectionID {
			continue
		}
		if oldest == nil || l.CreatedAt.Time.Before(oldest.CreatedAt.Time) {
			oldest = l
		}
	}
	if oldest == nil {
		return nil
	}
	oldest.IsDefault = true
	oldest.UpdatedAt = now()
	f.linkedGitlabProjectsByID[oldest.ID] = *oldest
	return nil
}

// SetLinkedGitlabProjectWebhookForOwner mirrors the SQL: records a
// successful webhook registration or rotation and clears any earlier
// registration error.
func (f *FakeQuerier) SetLinkedGitlabProjectWebhookForOwner(_ context.Context, arg db.SetLinkedGitlabProjectWebhookForOwnerParams) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(existing)
	if !ok || !f.hasRoleAtLeast(projectID, arg.OwnerUserID, "member") {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	existing.WebhookID = arg.WebhookID
	existing.EncryptedWebhookSecret = arg.EncryptedWebhookSecret
	existing.WebhookRegisteredAt = now()
	existing.WebhookRegistrationError = ""
	existing.UpdatedAt = now()
	f.storeLinkedGitlabProject(existing)
	return existing, nil
}

// SetLinkedGitlabProjectWebhookErrorForOwner mirrors the SQL: records why a
// webhook registration or repair attempt failed, leaving any existing
// webhook_id untouched.
func (f *FakeQuerier) SetLinkedGitlabProjectWebhookErrorForOwner(_ context.Context, arg db.SetLinkedGitlabProjectWebhookErrorForOwnerParams) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	projectID, ok := f.linkedProjectProjectID(existing)
	if !ok || !f.hasRoleAtLeast(projectID, arg.OwnerUserID, "member") {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	existing.WebhookRegistrationError = arg.WebhookRegistrationError
	existing.UpdatedAt = now()
	f.storeLinkedGitlabProject(existing)
	return existing, nil
}

// UpdateLinkedGitlabProjectLastSyncedAt mirrors the SQL: unscoped, called by
// the background worker (internal/projectsync).
func (f *FakeQuerier) UpdateLinkedGitlabProjectLastSyncedAt(_ context.Context, id uuid.UUID) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[id]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	existing.LastSyncedAt = now()
	existing.UpdatedAt = now()
	f.storeLinkedGitlabProject(existing)
	return existing, nil
}

// UpdateLinkedGitlabProjectInitialImportStatus mirrors the SQL: unscoped,
// called by the background worker (internal/projectsync).
func (f *FakeQuerier) UpdateLinkedGitlabProjectInitialImportStatus(_ context.Context, arg db.UpdateLinkedGitlabProjectInitialImportStatusParams) (db.LinkedGitlabProject, error) {
	existing, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	existing.InitialImportStatus = arg.InitialImportStatus
	existing.UpdatedAt = now()
	f.storeLinkedGitlabProject(existing)
	return existing, nil
}

// storeSyncJob inserts j if it is new, or overwrites the existing row in
// place (preserving its position in f.syncJobs) otherwise.
func (f *FakeQuerier) storeSyncJob(j db.SyncJob) {
	f.syncJobsByID[j.ID] = j
	if j.DedupeKey.Valid {
		f.syncJobsByDedupeKey[j.DedupeKey.String] = j.ID
	}
	for i, x := range f.syncJobs {
		if x.ID == j.ID {
			f.syncJobs[i] = j
			return
		}
	}
	f.syncJobs = append(f.syncJobs, j)
}

// EnqueueSyncJob mirrors the SQL's ON CONFLICT (dedupe_key) DO UPDATE ...
// WHERE status = 'pending': a collision with a pending job refreshes its
// payload in place, while a collision with a running/succeeded/failed job is
// left untouched and reported as pgx.ErrNoRows, matching what Postgres
// returns when the WHERE clause suppresses the update. internal/sync.Enqueue
// handles that by re-reading the existing row via GetSyncJobByDedupeKey.
func (f *FakeQuerier) EnqueueSyncJob(_ context.Context, arg db.EnqueueSyncJobParams) (db.SyncJob, error) {
	if arg.DedupeKey.Valid {
		if id, ok := f.syncJobsByDedupeKey[arg.DedupeKey.String]; ok {
			existing := f.syncJobsByID[id]
			if existing.Status != "pending" {
				return db.SyncJob{}, pgx.ErrNoRows
			}
			existing.Payload = arg.Payload
			existing.UpdatedAt = now()
			f.storeSyncJob(existing)
			return existing, nil
		}
	}

	j := db.SyncJob{
		ID:        uuid.New(),
		ProjectID: arg.ProjectID,
		TaskID:    arg.TaskID,
		Kind:      arg.Kind,
		Payload:   arg.Payload,
		DedupeKey: arg.DedupeKey,
		Status:    "pending",
		Attempts:  0,
		RunAfter:  now(),
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	f.storeSyncJob(j)
	return j, nil
}

func (f *FakeQuerier) GetSyncJobByDedupeKey(_ context.Context, dedupeKey pgtype.Text) (db.SyncJob, error) {
	if !dedupeKey.Valid {
		return db.SyncJob{}, pgx.ErrNoRows
	}
	id, ok := f.syncJobsByDedupeKey[dedupeKey.String]
	if !ok {
		return db.SyncJob{}, pgx.ErrNoRows
	}
	return f.syncJobsByID[id], nil
}

// ClaimPendingSyncJobs mirrors the SQL's SKIP LOCKED claim: due pending jobs
// are selected oldest-run_after-first, up to limit, and flipped to 'running'
// in the same call.
func (f *FakeQuerier) ClaimPendingSyncJobs(_ context.Context, limit int32) ([]db.SyncJob, error) {
	candidates := []db.SyncJob{}
	for _, j := range f.syncJobs {
		if j.Status == "pending" && !j.RunAfter.Time.After(time.Now()) {
			candidates = append(candidates, j)
		}
	}
	sort.SliceStable(candidates, func(i, k int) bool {
		return candidates[i].RunAfter.Time.Before(candidates[k].RunAfter.Time)
	})
	if int32(len(candidates)) > limit {
		candidates = candidates[:limit]
	}

	claimed := make([]db.SyncJob, 0, len(candidates))
	for _, j := range candidates {
		j.Status = "running"
		j.UpdatedAt = now()
		f.storeSyncJob(j)
		claimed = append(claimed, j)
	}
	return claimed, nil
}

// MarkSyncJobSucceeded also clears dedupe_key, mirroring the SQL, so a later
// enqueue with the same key is never permanently blocked by a job that
// already reached a terminal state.
func (f *FakeQuerier) MarkSyncJobSucceeded(_ context.Context, id uuid.UUID) error {
	j, ok := f.syncJobsByID[id]
	if !ok {
		return nil
	}
	f.clearSyncJobDedupeKey(j)
	j.Status = "succeeded"
	j.DedupeKey = pgtype.Text{}
	j.LastError = ""
	j.UpdatedAt = now()
	f.storeSyncJob(j)
	return nil
}

func (f *FakeQuerier) MarkSyncJobRetry(_ context.Context, arg db.MarkSyncJobRetryParams) error {
	j, ok := f.syncJobsByID[arg.ID]
	if !ok {
		return nil
	}
	j.Status = "pending"
	j.Attempts++
	j.RunAfter = arg.RunAfter
	j.LastError = arg.LastError
	j.UpdatedAt = now()
	f.storeSyncJob(j)
	return nil
}

// MarkSyncJobFailed also clears dedupe_key, mirroring the SQL, so a later
// enqueue with the same key is never permanently blocked by a job that
// already reached a terminal state.
func (f *FakeQuerier) MarkSyncJobFailed(_ context.Context, arg db.MarkSyncJobFailedParams) error {
	j, ok := f.syncJobsByID[arg.ID]
	if !ok {
		return nil
	}
	f.clearSyncJobDedupeKey(j)
	j.Status = "failed"
	j.Attempts++
	j.DedupeKey = pgtype.Text{}
	j.LastError = arg.LastError
	j.UpdatedAt = now()
	f.storeSyncJob(j)
	return nil
}

// clearSyncJobDedupeKey drops j's dedupe-key index entry before its terminal
// status is stored, keeping syncJobsByDedupeKey pointed only at jobs a
// caller can still collapse into.
func (f *FakeQuerier) clearSyncJobDedupeKey(j db.SyncJob) {
	if j.DedupeKey.Valid {
		delete(f.syncJobsByDedupeKey, j.DedupeKey.String)
	}
}

// ReclaimStaleRunningSyncJobs mirrors the SQL: a 'running' job whose
// updated_at predates updatedBefore was left behind by a process that died
// mid-execution, so it is returned to 'pending'.
func (f *FakeQuerier) ReclaimStaleRunningSyncJobs(_ context.Context, updatedBefore pgtype.Timestamptz) (int64, error) {
	var affected int64
	for _, j := range f.syncJobs {
		if j.Status != "running" || !j.UpdatedAt.Time.Before(updatedBefore.Time) {
			continue
		}
		j.Status = "pending"
		j.UpdatedAt = now()
		f.storeSyncJob(j)
		affected++
	}
	return affected, nil
}

// GetLatestSyncJobForTask returns taskID's most recently created sync job,
// mirroring the SQL's ORDER BY created_at DESC LIMIT 1. f.syncJobs is kept in
// insertion order, so the last match found scanning from the end is the most
// recent one.
func (f *FakeQuerier) GetLatestSyncJobForTask(_ context.Context, taskID uuid.UUID) (db.SyncJob, error) {
	for i := len(f.syncJobs) - 1; i >= 0; i-- {
		j := f.syncJobs[i]
		if j.TaskID.Valid && j.TaskID.Bytes == taskID {
			return j, nil
		}
	}
	return db.SyncJob{}, pgx.ErrNoRows
}

// RetryFailedSyncJobForTask mirrors the SQL: it finds taskID's most recent
// pending-or-failed job (same scan order as GetLatestSyncJobForTask) and
// resets it to a fresh pending attempt. No match is reported as pgx.ErrNoRows,
// which internal/task.Service.RetrySync maps to ErrSyncNotFailed.
func (f *FakeQuerier) RetryFailedSyncJobForTask(_ context.Context, taskID uuid.UUID) (db.SyncJob, error) {
	for i := len(f.syncJobs) - 1; i >= 0; i-- {
		j := f.syncJobs[i]
		if !j.TaskID.Valid || j.TaskID.Bytes != taskID {
			continue
		}
		if j.Status != "pending" && j.Status != "failed" {
			continue
		}
		j.Status = "pending"
		j.Attempts = 0
		j.RunAfter = now()
		j.LastError = ""
		f.storeSyncJob(j)
		return j, nil
	}
	return db.SyncJob{}, pgx.ErrNoRows
}

// SeedSyncJob inserts a ready-made sync job directly in the given status,
// bypassing Enqueue/Claim. Use it in tests that need a pre-existing job in a
// specific state (e.g. a stale 'running' job for reclaim tests, or an aged
// 'pending' one for queue-depth gauge tests) without exercising the
// transition that would normally produce it. created_at is also backdated
// to updatedAt, since every caller passing a past timestamp means "this job
// has sat here since then," not just "was last touched then."
func (f *FakeQuerier) SeedSyncJob(projectID uuid.UUID, kind, status string, updatedAt time.Time) db.SyncJob {
	j := db.SyncJob{
		ID:        uuid.New(),
		ProjectID: projectID,
		Kind:      kind,
		Payload:   []byte("{}"),
		Status:    status,
		RunAfter:  now(),
		CreatedAt: pgtype.Timestamptz{Time: updatedAt, Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: updatedAt, Valid: true},
	}
	f.storeSyncJob(j)
	return j
}

// SeedSyncJobForTask is SeedSyncJob for a job tied to a task (e.g. an
// issue.create job for a task with no task_gitlab_links row yet), so tests
// can exercise gitlabInfoForTask's fallback to sync_jobs without running a
// real worker.
func (f *FakeQuerier) SeedSyncJobForTask(taskID, projectID uuid.UUID, kind, status, lastError string) db.SyncJob {
	j := db.SyncJob{
		ID:        uuid.New(),
		ProjectID: projectID,
		TaskID:    pgtype.UUID{Bytes: taskID, Valid: true},
		Kind:      kind,
		Payload:   []byte("{}"),
		Status:    status,
		LastError: lastError,
		RunAfter:  now(),
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	f.storeSyncJob(j)
	return j
}

// SyncJobCount returns the number of sync_jobs rows currently stored, so
// tests can assert a dedupe collision reused a row instead of creating a
// duplicate.
func (f *FakeQuerier) SyncJobCount() int {
	return len(f.syncJobs)
}

// GetPendingSyncJobQueueStats mirrors the SQL's count(*)/min(created_at) over
// status = 'pending': oldest_pending_at comes back invalid (NULL) when no
// job is pending, matching what min() over zero rows returns in Postgres.
func (f *FakeQuerier) GetPendingSyncJobQueueStats(_ context.Context) (db.GetPendingSyncJobQueueStatsRow, error) {
	stats := db.GetPendingSyncJobQueueStatsRow{}
	for _, j := range f.syncJobs {
		if j.Status != "pending" {
			continue
		}
		stats.PendingCount++
		if !stats.OldestPendingAt.Valid || j.CreatedAt.Time.Before(stats.OldestPendingAt.Time) {
			stats.OldestPendingAt = j.CreatedAt
		}
	}
	return stats, nil
}

// GetSyncJob returns the sync job stored under id, so tests can assert on
// worker-driven state transitions (status, attempts, last_error, ...)
// directly.
func (f *FakeQuerier) GetSyncJob(id uuid.UUID) (db.SyncJob, bool) {
	j, ok := f.syncJobsByID[id]
	return j, ok
}

// SyncJobsForTask returns every sync job enqueued for taskID, insertion
// order, so tests can assert on what internal/task enqueued (kind, payload,
// dedupe_key) without running a real worker.
func (f *FakeQuerier) SyncJobsForTask(taskID uuid.UUID) []db.SyncJob {
	var jobs []db.SyncJob
	for _, j := range f.syncJobs {
		if j.TaskID.Valid && j.TaskID.Bytes == taskID {
			jobs = append(jobs, j)
		}
	}
	return jobs
}

// ListFailedSyncJobsByProjectForOwner mirrors the SQL's join through
// projects: a projectID that doesn't exist or belongs to another owner
// simply matches no rows, newest first.
func (f *FakeQuerier) ListFailedSyncJobsByProjectForOwner(_ context.Context, arg db.ListFailedSyncJobsByProjectForOwnerParams) ([]db.SyncJob, error) {
	if _, ok := f.projectsByID[arg.ProjectID]; !ok || !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return []db.SyncJob{}, nil
	}
	out := []db.SyncJob{}
	for i := len(f.syncJobs) - 1; i >= 0; i-- {
		j := f.syncJobs[i]
		if j.ProjectID == arg.ProjectID && j.Status == "failed" {
			out = append(out, j)
		}
	}
	return out, nil
}

// GetSyncJobForOwner mirrors the SQL's join through projects: a job whose
// project doesn't exist or belongs to another owner is reported as
// pgx.ErrNoRows, the same as an unknown job ID.
func (f *FakeQuerier) GetSyncJobForOwner(_ context.Context, arg db.GetSyncJobForOwnerParams) (db.SyncJob, error) {
	j, ok := f.syncJobsByID[arg.ID]
	if !ok {
		return db.SyncJob{}, pgx.ErrNoRows
	}
	if _, ok := f.projectsByID[j.ProjectID]; !ok || !f.hasMembership(j.ProjectID, arg.OwnerUserID) {
		return db.SyncJob{}, pgx.ErrNoRows
	}
	return j, nil
}

// RetrySyncJob mirrors the SQL: only a job currently 'failed' is reset, a
// concurrent double-retry (or a retry racing the worker) reported as
// pgx.ErrNoRows.
func (f *FakeQuerier) RetrySyncJob(_ context.Context, id uuid.UUID) (db.SyncJob, error) {
	j, ok := f.syncJobsByID[id]
	if !ok || j.Status != "failed" {
		return db.SyncJob{}, pgx.ErrNoRows
	}
	j.Status = "pending"
	j.Attempts = 0
	j.RunAfter = now()
	j.LastError = ""
	f.storeSyncJob(j)
	return j, nil
}

// CreateTaskGitlabLink mirrors the SQL: task_id is the primary key, so a
// second insert for the same task is a unique-constraint violation, the
// same as it would be against real Postgres.
func (f *FakeQuerier) CreateTaskGitlabLink(_ context.Context, arg db.CreateTaskGitlabLinkParams) (db.TaskGitlabLink, error) {
	if _, ok := f.taskGitlabLinksByTaskID[arg.TaskID]; ok {
		return db.TaskGitlabLink{}, &pgconn.PgError{Code: "23505", ConstraintName: "task_gitlab_links_pkey"}
	}
	for _, l := range f.taskGitlabLinksByTaskID {
		if l.LinkedGitlabProjectID == arg.LinkedGitlabProjectID && l.GitlabIssueIid == arg.GitlabIssueIid {
			return db.TaskGitlabLink{}, &pgconn.PgError{Code: "23505", ConstraintName: "task_gitlab_links_linked_gitlab_project_id_gitlab_issue_iid_key"}
		}
	}
	l := db.TaskGitlabLink{
		TaskID:                arg.TaskID,
		LinkedGitlabProjectID: arg.LinkedGitlabProjectID,
		GitlabIssueID:         arg.GitlabIssueID,
		GitlabIssueIid:        arg.GitlabIssueIid,
		GitlabWebUrl:          arg.GitlabWebUrl,
		GitlabUpdatedAt:       arg.GitlabUpdatedAt,
		LastPushedFingerprint: arg.LastPushedFingerprint,
		SyncStatus:            "synced",
		LastSyncedAt:          now(),
	}
	f.taskGitlabLinksByTaskID[l.TaskID] = l
	return l, nil
}

func (f *FakeQuerier) GetTaskGitlabLinkByTaskID(_ context.Context, taskID uuid.UUID) (db.TaskGitlabLink, error) {
	l, ok := f.taskGitlabLinksByTaskID[taskID]
	if !ok {
		return db.TaskGitlabLink{}, pgx.ErrNoRows
	}
	return l, nil
}

// GetTaskGitlabLinkWithProjectPathByTaskID mirrors the SQL: the link plus
// its linked GitLab project's path_with_namespace, joined in.
func (f *FakeQuerier) GetTaskGitlabLinkWithProjectPathByTaskID(_ context.Context, taskID uuid.UUID) (db.GetTaskGitlabLinkWithProjectPathByTaskIDRow, error) {
	l, ok := f.taskGitlabLinksByTaskID[taskID]
	if !ok {
		return db.GetTaskGitlabLinkWithProjectPathByTaskIDRow{}, pgx.ErrNoRows
	}
	p, ok := f.linkedGitlabProjectsByID[l.LinkedGitlabProjectID]
	if !ok {
		return db.GetTaskGitlabLinkWithProjectPathByTaskIDRow{}, pgx.ErrNoRows
	}
	return db.GetTaskGitlabLinkWithProjectPathByTaskIDRow{
		TaskID:                l.TaskID,
		LinkedGitlabProjectID: l.LinkedGitlabProjectID,
		GitlabIssueID:         l.GitlabIssueID,
		GitlabIssueIid:        l.GitlabIssueIid,
		GitlabWebUrl:          l.GitlabWebUrl,
		GitlabUpdatedAt:       l.GitlabUpdatedAt,
		LastPushedFingerprint: l.LastPushedFingerprint,
		SyncStatus:            l.SyncStatus,
		LastError:             l.LastError,
		LastSyncedAt:          l.LastSyncedAt,
		PathWithNamespace:     p.PathWithNamespace,
	}, nil
}

// GetTaskGitlabLinkByLinkedProjectAndIID mirrors the SQL: a lookup keyed by
// the same columns as the 1:1 UNIQUE constraint, used by the inbound webhook
// apply pipeline (internal/webhookapply) to tell a known issue from an
// unknown one.
func (f *FakeQuerier) GetTaskGitlabLinkByLinkedProjectAndIID(_ context.Context, arg db.GetTaskGitlabLinkByLinkedProjectAndIIDParams) (db.TaskGitlabLink, error) {
	for _, l := range f.taskGitlabLinksByTaskID {
		if l.LinkedGitlabProjectID == arg.LinkedGitlabProjectID && l.GitlabIssueIid == arg.GitlabIssueIid {
			return l, nil
		}
	}
	return db.TaskGitlabLink{}, pgx.ErrNoRows
}

// MarkTaskGitlabLinkAppliedForTask mirrors the SQL: records a successful
// inbound apply (internal/webhookapply) without touching
// last_pushed_fingerprint, unlike MarkTaskGitlabLinkSyncedForTask.
func (f *FakeQuerier) MarkTaskGitlabLinkAppliedForTask(_ context.Context, arg db.MarkTaskGitlabLinkAppliedForTaskParams) (db.TaskGitlabLink, error) {
	l, ok := f.taskGitlabLinksByTaskID[arg.TaskID]
	if !ok {
		return db.TaskGitlabLink{}, pgx.ErrNoRows
	}
	if arg.GitlabUpdatedAt.Valid {
		// Mirrors the real query's COALESCE(sqlc.narg('gitlab_updated_at'),
		// gitlab_updated_at): a delivery whose updated_at didn't parse passes
		// NULL here and must never erase an already-recorded baseline (issue
		// #183).
		l.GitlabUpdatedAt = arg.GitlabUpdatedAt
	}
	l.SyncStatus = "synced"
	l.LastError = ""
	f.taskGitlabLinksByTaskID[arg.TaskID] = l
	return l, nil
}

func (f *FakeQuerier) MarkTaskGitlabLinkSyncedForTask(_ context.Context, arg db.MarkTaskGitlabLinkSyncedForTaskParams) (db.TaskGitlabLink, error) {
	l, ok := f.taskGitlabLinksByTaskID[arg.TaskID]
	if !ok {
		return db.TaskGitlabLink{}, pgx.ErrNoRows
	}
	l.GitlabUpdatedAt = arg.GitlabUpdatedAt
	l.LastPushedFingerprint = arg.LastPushedFingerprint
	l.SyncStatus = "synced"
	l.LastError = ""
	l.LastSyncedAt = now()
	f.taskGitlabLinksByTaskID[arg.TaskID] = l
	return l, nil
}

func (f *FakeQuerier) MarkTaskGitlabLinkFailedForTask(_ context.Context, arg db.MarkTaskGitlabLinkFailedForTaskParams) (db.TaskGitlabLink, error) {
	l, ok := f.taskGitlabLinksByTaskID[arg.TaskID]
	if !ok {
		return db.TaskGitlabLink{}, pgx.ErrNoRows
	}
	l.SyncStatus = "failed"
	l.LastError = arg.LastError
	f.taskGitlabLinksByTaskID[arg.TaskID] = l
	return l, nil
}

func (f *FakeQuerier) MarkTaskGitlabLinkPendingForTask(_ context.Context, taskID uuid.UUID) (db.TaskGitlabLink, error) {
	l, ok := f.taskGitlabLinksByTaskID[taskID]
	if !ok {
		return db.TaskGitlabLink{}, pgx.ErrNoRows
	}
	l.SyncStatus = "pending"
	l.LastError = ""
	f.taskGitlabLinksByTaskID[taskID] = l
	return l, nil
}

// SeedTaskGitlabLink inserts a ready-made GitLab link for taskID directly,
// bypassing HandleIssueCreate. Use it in tests that need a task already
// linked (e.g. to exercise issue.update/close/reopen) without exercising
// creation. Returns the stored row.
func (f *FakeQuerier) SeedTaskGitlabLink(taskID, linkedGitlabProjectID uuid.UUID, gitlabIssueIID int64) db.TaskGitlabLink {
	l := db.TaskGitlabLink{
		TaskID:                taskID,
		LinkedGitlabProjectID: linkedGitlabProjectID,
		GitlabIssueID:         gitlabIssueIID,
		GitlabIssueIid:        gitlabIssueIID,
		SyncStatus:            "synced",
		LastSyncedAt:          now(),
	}
	f.taskGitlabLinksByTaskID[taskID] = l
	return l
}

// SeedLinkedGitlabProject inserts a linked GitLab project row directly,
// bypassing Create, for tests that need a link with a known (already
// "registered") webhook secret without wiring up a full
// connection/project chain — GetLinkedGitlabProjectByID, the only query
// internal/webhookevent uses, is unscoped. Returns the stored row.
func (f *FakeQuerier) SeedLinkedGitlabProject(encryptedWebhookSecret []byte) db.LinkedGitlabProject {
	l := db.LinkedGitlabProject{
		ID:                     uuid.New(),
		GitlabConnectionID:     uuid.New(),
		GitlabProjectID:        1,
		PathWithNamespace:      "group/project",
		Name:                   "project",
		EncryptedWebhookSecret: encryptedWebhookSecret,
		InitialImportStatus:    "pending",
		CreatedAt:              now(),
		UpdatedAt:              now(),
	}
	f.storeLinkedGitlabProject(l)
	return l
}

func webhookEventKey(linkedGitlabProjectID uuid.UUID, deliveryUUID string) string {
	return linkedGitlabProjectID.String() + "\x00" + deliveryUUID
}

// storeWebhookEvent inserts e if it is new, or overwrites the existing row
// in place (preserving its position in f.webhookEvents) otherwise.
func (f *FakeQuerier) storeWebhookEvent(e db.WebhookEvent) {
	f.webhookEventsByID[e.ID] = e
	f.webhookEventsByKey[webhookEventKey(e.LinkedGitlabProjectID, e.DeliveryUuid)] = e
	for i, x := range f.webhookEvents {
		if x.ID == e.ID {
			f.webhookEvents[i] = e
			return
		}
	}
	f.webhookEvents = append(f.webhookEvents, e)
}

// CreateWebhookEvent mirrors the SQL: ON CONFLICT (linked_gitlab_project_id,
// delivery_uuid) DO NOTHING makes a duplicate delivery a no-op, reported as
// pgx.ErrNoRows exactly like the real driver does for a zero-row RETURNING.
func (f *FakeQuerier) CreateWebhookEvent(_ context.Context, arg db.CreateWebhookEventParams) (db.WebhookEvent, error) {
	key := webhookEventKey(arg.LinkedGitlabProjectID, arg.DeliveryUuid)
	if _, exists := f.webhookEventsByKey[key]; exists {
		return db.WebhookEvent{}, pgx.ErrNoRows
	}

	e := db.WebhookEvent{
		ID:                    uuid.New(),
		LinkedGitlabProjectID: arg.LinkedGitlabProjectID,
		DeliveryUuid:          arg.DeliveryUuid,
		EventName:             arg.EventName,
		ObjectKind:            arg.ObjectKind,
		GitlabIssueIid:        arg.GitlabIssueIid,
		Payload:               arg.Payload,
		GitlabUpdatedAt:       arg.GitlabUpdatedAt,
		Status:                arg.Status,
		SkipReason:            arg.SkipReason,
		ReceivedAt:            now(),
	}
	f.storeWebhookEvent(e)
	return e, nil
}

// ClaimNextPendingWebhookEvent mirrors the SQL's ordering (oldest
// received_at first); f.webhookEvents is kept in insertion order, so the
// first 'pending' row scanning from the start is the oldest one. The fake
// has no real row locking (SKIP LOCKED), which is fine: that guarantee is
// verified against real Postgres in internal/webhookapply's integration
// test, not here (docs/testing.md).
func (f *FakeQuerier) ClaimNextPendingWebhookEvent(_ context.Context) (db.WebhookEvent, error) {
	for _, e := range f.webhookEvents {
		if e.Status == "pending" {
			return e, nil
		}
	}
	return db.WebhookEvent{}, pgx.ErrNoRows
}

// MarkWebhookEventProcessed mirrors the SQL.
func (f *FakeQuerier) MarkWebhookEventProcessed(_ context.Context, id uuid.UUID) error {
	e, ok := f.webhookEventsByID[id]
	if !ok {
		return nil
	}
	e.Status = "processed"
	e.ErrorMessage = ""
	e.ProcessedAt = now()
	f.storeWebhookEvent(e)
	return nil
}

// MarkWebhookEventSkipped mirrors the SQL.
func (f *FakeQuerier) MarkWebhookEventSkipped(_ context.Context, arg db.MarkWebhookEventSkippedParams) error {
	e, ok := f.webhookEventsByID[arg.ID]
	if !ok {
		return nil
	}
	e.Status = "skipped"
	e.SkipReason = arg.SkipReason
	e.ProcessedAt = now()
	f.storeWebhookEvent(e)
	return nil
}

// MarkWebhookEventFailed mirrors the SQL.
func (f *FakeQuerier) MarkWebhookEventFailed(_ context.Context, arg db.MarkWebhookEventFailedParams) error {
	e, ok := f.webhookEventsByID[arg.ID]
	if !ok {
		return nil
	}
	e.Status = "failed"
	e.ErrorMessage = arg.ErrorMessage
	e.ProcessedAt = now()
	f.storeWebhookEvent(e)
	return nil
}

// SeedWebhookEvent inserts a ready-made pending webhook_events row directly,
// bypassing the receiver's Record path, for tests that exercise the apply
// pipeline (internal/webhookapply) in isolation. Returns the stored row.
func (f *FakeQuerier) SeedWebhookEvent(linkedGitlabProjectID uuid.UUID, payload []byte) db.WebhookEvent {
	e := db.WebhookEvent{
		ID:                    uuid.New(),
		LinkedGitlabProjectID: linkedGitlabProjectID,
		DeliveryUuid:          uuid.NewString(),
		EventName:             "Issue Hook",
		ObjectKind:            "issue",
		Payload:               payload,
		Status:                "pending",
		ReceivedAt:            now(),
	}
	f.storeWebhookEvent(e)
	return e
}

// GetWebhookEvent returns the webhook event stored under id, so tests can
// assert on the apply pipeline's outcome (status, skip_reason,
// error_message) directly.
func (f *FakeQuerier) GetWebhookEvent(id uuid.UUID) (db.WebhookEvent, bool) {
	e, ok := f.webhookEventsByID[id]
	return e, ok
}

// SetTaskForTest overwrites an already-seeded tasks row in place, for tests
// that need to set fields SeedTask doesn't take (e.g. due_on/status for
// internal/notification's digest queries) without exercising a full
// create-then-update round trip.
func (f *FakeQuerier) SetTaskForTest(t db.Task) {
	f.storeTask(t)
}

// SetWebhookEventForTest overwrites an already-seeded webhook_events row in
// place, for tests that need to set status/processed_at directly (e.g. to
// exercise the list/retry/cleanup read side) rather than going through
// Record or the apply pipeline.
func (f *FakeQuerier) SetWebhookEventForTest(e db.WebhookEvent) {
	f.storeWebhookEvent(e)
}

// WebhookEventsForLink returns every recorded webhook_events row for
// linkedGitlabProjectID, insertion order, for test assertions.
func (f *FakeQuerier) WebhookEventsForLink(linkedGitlabProjectID uuid.UUID) []db.WebhookEvent {
	items := []db.WebhookEvent{}
	for _, e := range f.webhookEvents {
		if e.LinkedGitlabProjectID == linkedGitlabProjectID {
			items = append(items, e)
		}
	}
	return items
}

// ListWebhookEventsByLinkedGitlabProjectID mirrors the SQL's ORDER BY
// received_at DESC, optional status filter, and LIMIT/OFFSET paging.
func (f *FakeQuerier) ListWebhookEventsByLinkedGitlabProjectID(_ context.Context, arg db.ListWebhookEventsByLinkedGitlabProjectIDParams) ([]db.WebhookEvent, error) {
	items := []db.WebhookEvent{}
	for _, e := range f.webhookEvents {
		if e.LinkedGitlabProjectID != arg.LinkedGitlabProjectID {
			continue
		}
		if arg.Status != "" && e.Status != arg.Status {
			continue
		}
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ReceivedAt.Time.After(items[j].ReceivedAt.Time)
	})

	offset := int(arg.OffsetCount)
	if offset > len(items) {
		offset = len(items)
	}
	items = items[offset:]
	limit := int(arg.LimitCount)
	if limit >= 0 && limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

// GetWebhookEventByLinkedGitlabProjectIDAndID mirrors the SQL: an event
// belonging to a different link is reported as missing, the same way a
// foreign linked_gitlab_projects row is.
func (f *FakeQuerier) GetWebhookEventByLinkedGitlabProjectIDAndID(_ context.Context, arg db.GetWebhookEventByLinkedGitlabProjectIDAndIDParams) (db.WebhookEvent, error) {
	e, ok := f.webhookEventsByID[arg.ID]
	if !ok || e.LinkedGitlabProjectID != arg.LinkedGitlabProjectID {
		return db.WebhookEvent{}, pgx.ErrNoRows
	}
	return e, nil
}

// RetryWebhookEvent mirrors the SQL: only a 'failed' event flips back to
// 'pending'; any other status (or an unknown id) is pgx.ErrNoRows.
func (f *FakeQuerier) RetryWebhookEvent(_ context.Context, id uuid.UUID) (db.WebhookEvent, error) {
	e, ok := f.webhookEventsByID[id]
	if !ok || e.Status != "failed" {
		return db.WebhookEvent{}, pgx.ErrNoRows
	}
	e.Status = "pending"
	e.ErrorMessage = ""
	e.ProcessedAt = pgtype.Timestamptz{}
	f.storeWebhookEvent(e)
	return e, nil
}

// DeleteProcessedWebhookEventsOlderThan mirrors the SQL: only 'processed'
// rows older than cutoff are removed.
func (f *FakeQuerier) DeleteProcessedWebhookEventsOlderThan(_ context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	var kept []db.WebhookEvent
	var deleted int64
	for _, e := range f.webhookEvents {
		if e.Status == "processed" && e.ProcessedAt.Valid && e.ProcessedAt.Time.Before(cutoff.Time) {
			delete(f.webhookEventsByID, e.ID)
			delete(f.webhookEventsByKey, webhookEventKey(e.LinkedGitlabProjectID, e.DeliveryUuid))
			deleted++
			continue
		}
		kept = append(kept, e)
	}
	f.webhookEvents = kept
	return deleted, nil
}

// storeGitlabSyncRun inserts r if it is new, or overwrites the existing row
// in place (preserving its position in f.gitlabSyncRuns) otherwise.
func (f *FakeQuerier) storeGitlabSyncRun(r db.GitlabSyncRun) {
	f.gitlabSyncRunsByID[r.ID] = r
	for i, x := range f.gitlabSyncRuns {
		if x.ID == r.ID {
			f.gitlabSyncRuns[i] = r
			return
		}
	}
	f.gitlabSyncRuns = append(f.gitlabSyncRuns, r)
}

// CreateGitlabSyncRun mirrors the SQL: the partial UNIQUE index on
// (linked_gitlab_project_id) WHERE completed_at IS NULL (migration 000006)
// means a second run for a link that already has one in flight is a
// unique-constraint violation, which internal/projectsync maps to
// ErrRunInProgress (HTTP 409).
func (f *FakeQuerier) CreateGitlabSyncRun(_ context.Context, arg db.CreateGitlabSyncRunParams) (db.GitlabSyncRun, error) {
	for _, r := range f.gitlabSyncRuns {
		if r.LinkedGitlabProjectID == arg.LinkedGitlabProjectID && !r.CompletedAt.Valid {
			return db.GitlabSyncRun{}, &pgconn.PgError{Code: "23505", ConstraintName: "idx_gitlab_sync_runs_one_running_per_link"}
		}
	}
	r := db.GitlabSyncRun{
		ID:                    uuid.New(),
		LinkedGitlabProjectID: arg.LinkedGitlabProjectID,
		Kind:                  arg.Kind,
		Status:                "running",
		StartedAt:             now(),
		CreatedAt:             now(),
	}
	f.storeGitlabSyncRun(r)
	return r, nil
}

// CompleteGitlabSyncRun mirrors the SQL.
func (f *FakeQuerier) CompleteGitlabSyncRun(_ context.Context, arg db.CompleteGitlabSyncRunParams) (db.GitlabSyncRun, error) {
	r, ok := f.gitlabSyncRunsByID[arg.ID]
	if !ok {
		return db.GitlabSyncRun{}, pgx.ErrNoRows
	}
	r.Status = "succeeded"
	r.IssuesSeen = arg.IssuesSeen
	r.IssuesCreated = arg.IssuesCreated
	r.IssuesUpdated = arg.IssuesUpdated
	r.CompletedAt = now()
	f.storeGitlabSyncRun(r)
	return r, nil
}

// FailGitlabSyncRun mirrors the SQL.
func (f *FakeQuerier) FailGitlabSyncRun(_ context.Context, arg db.FailGitlabSyncRunParams) (db.GitlabSyncRun, error) {
	r, ok := f.gitlabSyncRunsByID[arg.ID]
	if !ok {
		return db.GitlabSyncRun{}, pgx.ErrNoRows
	}
	r.Status = "failed"
	r.IssuesSeen = arg.IssuesSeen
	r.IssuesCreated = arg.IssuesCreated
	r.IssuesUpdated = arg.IssuesUpdated
	r.ErrorMessage = arg.ErrorMessage
	r.CompletedAt = now()
	f.storeGitlabSyncRun(r)
	return r, nil
}

// GetGitlabSyncRunByID mirrors the SQL: unscoped, for the background worker.
func (f *FakeQuerier) GetGitlabSyncRunByID(_ context.Context, id uuid.UUID) (db.GitlabSyncRun, error) {
	r, ok := f.gitlabSyncRunsByID[id]
	if !ok {
		return db.GitlabSyncRun{}, pgx.ErrNoRows
	}
	return r, nil
}

// ListGitlabSyncRunsByLinkedGitlabProjectID mirrors the SQL's ORDER BY
// created_at DESC.
func (f *FakeQuerier) ListGitlabSyncRunsByLinkedGitlabProjectID(_ context.Context, linkedGitlabProjectID uuid.UUID) ([]db.GitlabSyncRun, error) {
	items := []db.GitlabSyncRun{}
	for _, r := range f.gitlabSyncRuns {
		if r.LinkedGitlabProjectID == linkedGitlabProjectID {
			items = append(items, r)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Time.After(items[j].CreatedAt.Time)
	})
	return items, nil
}

// CreateRepository mirrors the SQL: linked_gitlab_project_id is UNIQUE.
func (f *FakeQuerier) CreateRepository(_ context.Context, arg db.CreateRepositoryParams) (db.Repository, error) {
	if _, ok := f.repositoriesByLinkedGitlabProjectID[arg.LinkedGitlabProjectID]; ok {
		return db.Repository{}, &pgconn.PgError{Code: "23505", ConstraintName: "repositories_linked_gitlab_project_id_key"}
	}
	r := db.Repository{
		ID:                    uuid.New(),
		LinkedGitlabProjectID: arg.LinkedGitlabProjectID,
		Name:                  arg.Name,
		FullName:              arg.FullName,
		Description:           arg.Description,
		IsPrivate:             arg.IsPrivate,
		DefaultBranch:         arg.DefaultBranch,
		HtmlUrl:               arg.HtmlUrl,
		IsActive:              true,
		CreatedAt:             now(),
		UpdatedAt:             now(),
	}
	f.repositoriesByID[r.ID] = r
	f.repositoriesByLinkedGitlabProjectID[r.LinkedGitlabProjectID] = r
	return r, nil
}

// GetRepositoryByLinkedGitlabProjectID mirrors the SQL: unscoped, for the
// background worker.
func (f *FakeQuerier) GetRepositoryByLinkedGitlabProjectID(_ context.Context, linkedGitlabProjectID uuid.UUID) (db.Repository, error) {
	r, ok := f.repositoriesByLinkedGitlabProjectID[linkedGitlabProjectID]
	if !ok {
		return db.Repository{}, pgx.ErrNoRows
	}
	return r, nil
}

// GetRepositoryByID mirrors the SQL.
func (f *FakeQuerier) GetRepositoryByID(_ context.Context, id uuid.UUID) (db.Repository, error) {
	r, ok := f.repositoriesByID[id]
	if !ok {
		return db.Repository{}, pgx.ErrNoRows
	}
	return r, nil
}

// storeMergeRequest inserts m if it is new, or overwrites the existing row
// in place otherwise.
func (f *FakeQuerier) storeMergeRequest(m db.MergeRequest) {
	f.mergeRequestsByID[m.ID] = m
	f.mergeRequestsByGitlabMergeRequestID[m.GitlabMergeRequestID] = m.ID
}

// CreateMergeRequest mirrors the SQL: gitlab_merge_request_id is UNIQUE.
func (f *FakeQuerier) CreateMergeRequest(_ context.Context, arg db.CreateMergeRequestParams) (db.MergeRequest, error) {
	if _, ok := f.mergeRequestsByGitlabMergeRequestID[arg.GitlabMergeRequestID]; ok {
		return db.MergeRequest{}, &pgconn.PgError{Code: "23505", ConstraintName: "merge_requests_gitlab_merge_request_id_key"}
	}
	m := db.MergeRequest{
		ID:                   uuid.New(),
		RepositoryID:         arg.RepositoryID,
		GitlabMergeRequestID: arg.GitlabMergeRequestID,
		Number:               arg.Number,
		Title:                arg.Title,
		State:                arg.State,
		IsDraft:              arg.IsDraft,
		AuthorGitlabUsername: arg.AuthorGitlabUsername,
		AuthorAvatarUrl:      arg.AuthorAvatarUrl,
		BaseBranch:           arg.BaseBranch,
		HeadBranch:           arg.HeadBranch,
		GitlabCreatedAt:      arg.GitlabCreatedAt,
		GitlabUpdatedAt:      arg.GitlabUpdatedAt,
		MergedAt:             arg.MergedAt,
		ClosedAt:             arg.ClosedAt,
		HtmlUrl:              arg.HtmlUrl,
		PipelineStatus:       arg.PipelineStatus,
		PipelineID:           arg.PipelineID,
		PipelineUpdatedAt:    arg.PipelineUpdatedAt,
		CreatedAt:            now(),
		UpdatedAt:            now(),
	}
	f.storeMergeRequest(m)
	return m, nil
}

// GetMergeRequestByGitlabMergeRequestID mirrors the SQL.
func (f *FakeQuerier) GetMergeRequestByGitlabMergeRequestID(_ context.Context, gitlabMergeRequestID int64) (db.MergeRequest, error) {
	id, ok := f.mergeRequestsByGitlabMergeRequestID[gitlabMergeRequestID]
	if !ok {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	return f.mergeRequestsByID[id], nil
}

// UpdateMergeRequest mirrors the SQL: first_reviewed_at and task_id are
// deliberately left untouched.
func (f *FakeQuerier) UpdateMergeRequest(_ context.Context, arg db.UpdateMergeRequestParams) (db.MergeRequest, error) {
	id, ok := f.mergeRequestsByGitlabMergeRequestID[arg.GitlabMergeRequestID]
	if !ok {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	m := f.mergeRequestsByID[id]
	m.Title = arg.Title
	m.State = arg.State
	m.IsDraft = arg.IsDraft
	m.AuthorGitlabUsername = arg.AuthorGitlabUsername
	m.AuthorAvatarUrl = arg.AuthorAvatarUrl
	m.BaseBranch = arg.BaseBranch
	m.HeadBranch = arg.HeadBranch
	if arg.GitlabUpdatedAt.Valid {
		// Mirrors the real query's COALESCE($9, gitlab_updated_at): a
		// delivery whose updated_at didn't parse passes NULL here and must
		// never erase an already-recorded baseline (issue #183).
		m.GitlabUpdatedAt = arg.GitlabUpdatedAt
	}
	m.MergedAt = arg.MergedAt
	m.ClosedAt = arg.ClosedAt
	m.HtmlUrl = arg.HtmlUrl
	m.PipelineStatus = arg.PipelineStatus
	m.PipelineID = arg.PipelineID
	m.PipelineUpdatedAt = arg.PipelineUpdatedAt
	m.UpdatedAt = now()
	f.storeMergeRequest(m)
	return m, nil
}

// UpdateMergeRequestFirstReviewedAt mirrors the SQL: a no-op once already set.
func (f *FakeQuerier) UpdateMergeRequestFirstReviewedAt(_ context.Context, arg db.UpdateMergeRequestFirstReviewedAtParams) (db.MergeRequest, error) {
	m, ok := f.mergeRequestsByID[arg.ID]
	if !ok || m.FirstReviewedAt.Valid {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	m.FirstReviewedAt = arg.FirstReviewedAt
	f.storeMergeRequest(m)
	return m, nil
}

// GetMergeRequestByRepositoryAndNumber mirrors the SQL.
func (f *FakeQuerier) GetMergeRequestByRepositoryAndNumber(_ context.Context, arg db.GetMergeRequestByRepositoryAndNumberParams) (db.MergeRequest, error) {
	for _, id := range f.mergeRequestsByGitlabMergeRequestID {
		m := f.mergeRequestsByID[id]
		if m.RepositoryID == arg.RepositoryID && m.Number == arg.Number {
			return m, nil
		}
	}
	return db.MergeRequest{}, pgx.ErrNoRows
}

// UpdateMergeRequestPipeline mirrors the SQL.
func (f *FakeQuerier) UpdateMergeRequestPipeline(_ context.Context, arg db.UpdateMergeRequestPipelineParams) (db.MergeRequest, error) {
	m, ok := f.mergeRequestsByID[arg.ID]
	if !ok {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	m.PipelineStatus = arg.PipelineStatus
	m.PipelineID = arg.PipelineID
	m.PipelineUpdatedAt = arg.PipelineUpdatedAt
	m.UpdatedAt = now()
	f.storeMergeRequest(m)
	return m, nil
}

// UpdateMergeRequestTaskID mirrors the SQL.
func (f *FakeQuerier) UpdateMergeRequestTaskID(_ context.Context, arg db.UpdateMergeRequestTaskIDParams) (db.MergeRequest, error) {
	m, ok := f.mergeRequestsByID[arg.ID]
	if !ok {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	m.TaskID = arg.TaskID
	f.storeMergeRequest(m)
	return m, nil
}

// SeedMergeRequest inserts a ready-made merge request directly, bypassing
// CreateMergeRequest's uniqueness plumbing, for tests that need one to
// already exist. gitlabMergeRequestID must be unique across the test.
func (f *FakeQuerier) SeedMergeRequest(repositoryID uuid.UUID, gitlabMergeRequestID int64, number int32, title, state string) db.MergeRequest {
	m := db.MergeRequest{
		ID:                   uuid.New(),
		RepositoryID:         repositoryID,
		GitlabMergeRequestID: gitlabMergeRequestID,
		Number:               number,
		Title:                title,
		State:                state,
		GitlabCreatedAt:      pgtype.Timestamptz{Time: now().Time, Valid: true},
		GitlabUpdatedAt:      pgtype.Timestamptz{Time: now().Time, Valid: true},
		CreatedAt:            now(),
		UpdatedAt:            now(),
	}
	f.storeMergeRequest(m)
	return m
}

// projectIDForRepository walks repositories -> linked_gitlab_projects ->
// gitlab_connections to the app project a repository belongs to, mirroring
// the join ListMergeRequestsByProject/GetMergeRequestForOwner/
// GetMergeRequestProjectID's SQL performs.
func (f *FakeQuerier) projectIDForRepository(repositoryID uuid.UUID) (uuid.UUID, bool) {
	repo, ok := f.repositoriesByID[repositoryID]
	if !ok {
		return uuid.UUID{}, false
	}
	link, ok := f.linkedGitlabProjectsByID[repo.LinkedGitlabProjectID]
	if !ok {
		return uuid.UUID{}, false
	}
	conn, ok := f.gitlabConnectionsByID[link.GitlabConnectionID]
	if !ok {
		return uuid.UUID{}, false
	}
	return conn.ProjectID, true
}

// ListMergeRequestsByProject mirrors the SQL: scoped to projectID via
// repositories/linked_gitlab_projects/gitlab_connections, gated by
// ownerUserID's project membership, then state/author/task_id/since/until
// filters, sorted by gitlab_updated_at or gitlab_created_at DESC (created_at
// as tiebreak).
func (f *FakeQuerier) ListMergeRequestsByProject(_ context.Context, arg db.ListMergeRequestsByProjectParams) ([]db.MergeRequest, error) {
	if !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return []db.MergeRequest{}, nil
	}
	items := []db.MergeRequest{}
	for _, m := range f.mergeRequestsByID {
		projectID, ok := f.projectIDForRepository(m.RepositoryID)
		if !ok || projectID != arg.ProjectID {
			continue
		}
		if arg.State != "" && m.State != arg.State {
			continue
		}
		if arg.Author != "" && m.AuthorGitlabUsername != arg.Author {
			continue
		}
		if arg.TaskID.Valid && m.TaskID != arg.TaskID {
			continue
		}
		if arg.Since.Valid && (!m.GitlabCreatedAt.Valid || m.GitlabCreatedAt.Time.Before(arg.Since.Time)) {
			continue
		}
		if arg.Until.Valid && (!m.GitlabCreatedAt.Valid || m.GitlabCreatedAt.Time.After(arg.Until.Time)) {
			continue
		}
		items = append(items, m)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		primary := func(m db.MergeRequest) pgtype.Timestamptz {
			if arg.SortByUpdated {
				return m.GitlabUpdatedAt
			}
			return m.GitlabCreatedAt
		}
		pa, pb := primary(a), primary(b)
		switch {
		case pa.Valid && pb.Valid && !pa.Time.Equal(pb.Time):
			return pa.Time.After(pb.Time)
		case pa.Valid != pb.Valid:
			return pa.Valid
		default:
			return a.CreatedAt.Time.After(b.CreatedAt.Time)
		}
	})
	return items, nil
}

// ListMergeRequestsForMetrics mirrors the SQL: same project-membership
// scoping and since/until bounding as ListMergeRequestsByProject, without
// the state/author/task_id/sort filters that view alone needs.
func (f *FakeQuerier) ListMergeRequestsForMetrics(_ context.Context, arg db.ListMergeRequestsForMetricsParams) ([]db.ListMergeRequestsForMetricsRow, error) {
	if !f.hasMembership(arg.ProjectID, arg.OwnerUserID) {
		return []db.ListMergeRequestsForMetricsRow{}, nil
	}
	items := []db.ListMergeRequestsForMetricsRow{}
	for _, m := range f.mergeRequestsByID {
		projectID, ok := f.projectIDForRepository(m.RepositoryID)
		if !ok || projectID != arg.ProjectID {
			continue
		}
		if arg.Since.Valid && (!m.GitlabCreatedAt.Valid || m.GitlabCreatedAt.Time.Before(arg.Since.Time)) {
			continue
		}
		if arg.Until.Valid && (!m.GitlabCreatedAt.Valid || m.GitlabCreatedAt.Time.After(arg.Until.Time)) {
			continue
		}
		items = append(items, db.ListMergeRequestsForMetricsRow{
			State:           m.State,
			Additions:       m.Additions,
			Deletions:       m.Deletions,
			ChangedFiles:    m.ChangedFiles,
			GitlabCreatedAt: m.GitlabCreatedAt,
			MergedAt:        m.MergedAt,
			FirstReviewedAt: m.FirstReviewedAt,
			PipelineStatus:  m.PipelineStatus,
		})
	}
	return items, nil
}

// GetMergeRequestForOwner mirrors the SQL: same project-membership scoping
// as ListMergeRequestsByProject, narrowed to a single id.
func (f *FakeQuerier) GetMergeRequestForOwner(_ context.Context, arg db.GetMergeRequestForOwnerParams) (db.MergeRequest, error) {
	m, ok := f.mergeRequestsByID[arg.ID]
	if !ok {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	projectID, ok := f.projectIDForRepository(m.RepositoryID)
	if !ok || !f.hasMembership(projectID, arg.OwnerUserID) {
		return db.MergeRequest{}, pgx.ErrNoRows
	}
	return m, nil
}

// GetMergeRequestProjectID mirrors the SQL: unscoped, for
// requireTokenResourceProject.
func (f *FakeQuerier) GetMergeRequestProjectID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	m, ok := f.mergeRequestsByID[id]
	if !ok {
		return uuid.UUID{}, pgx.ErrNoRows
	}
	projectID, ok := f.projectIDForRepository(m.RepositoryID)
	if !ok {
		return uuid.UUID{}, pgx.ErrNoRows
	}
	return projectID, nil
}

// storeRepositorySyncRun inserts r if it is new, or overwrites the existing
// row in place (preserving its position) otherwise.
func (f *FakeQuerier) storeRepositorySyncRun(r db.RepositorySyncRun) {
	f.repositorySyncRunsByID[r.ID] = r
	for i, x := range f.repositorySyncRuns {
		if x.ID == r.ID {
			f.repositorySyncRuns[i] = r
			return
		}
	}
	f.repositorySyncRuns = append(f.repositorySyncRuns, r)
}

// CreateRepositorySyncRun mirrors the SQL: the partial UNIQUE index on
// (repository_id) WHERE completed_at IS NULL (migration 000019).
func (f *FakeQuerier) CreateRepositorySyncRun(_ context.Context, arg db.CreateRepositorySyncRunParams) (db.RepositorySyncRun, error) {
	for _, r := range f.repositorySyncRuns {
		if r.RepositoryID == arg.RepositoryID && !r.CompletedAt.Valid {
			return db.RepositorySyncRun{}, &pgconn.PgError{Code: "23505", ConstraintName: "idx_repository_sync_runs_one_running_per_repository"}
		}
	}
	r := db.RepositorySyncRun{
		ID:           uuid.New(),
		RepositoryID: arg.RepositoryID,
		Kind:         arg.Kind,
		Status:       "running",
		StartedAt:    now(),
		CreatedAt:    now(),
	}
	f.storeRepositorySyncRun(r)
	return r, nil
}

// CompleteRepositorySyncRun mirrors the SQL.
func (f *FakeQuerier) CompleteRepositorySyncRun(_ context.Context, arg db.CompleteRepositorySyncRunParams) (db.RepositorySyncRun, error) {
	r, ok := f.repositorySyncRunsByID[arg.ID]
	if !ok {
		return db.RepositorySyncRun{}, pgx.ErrNoRows
	}
	r.Status = "succeeded"
	r.MrsSeen = arg.MrsSeen
	r.MrsCreated = arg.MrsCreated
	r.MrsUpdated = arg.MrsUpdated
	r.CompletedAt = now()
	f.storeRepositorySyncRun(r)
	return r, nil
}

// FailRepositorySyncRun mirrors the SQL.
func (f *FakeQuerier) FailRepositorySyncRun(_ context.Context, arg db.FailRepositorySyncRunParams) (db.RepositorySyncRun, error) {
	r, ok := f.repositorySyncRunsByID[arg.ID]
	if !ok {
		return db.RepositorySyncRun{}, pgx.ErrNoRows
	}
	r.Status = "failed"
	r.MrsSeen = arg.MrsSeen
	r.MrsCreated = arg.MrsCreated
	r.MrsUpdated = arg.MrsUpdated
	r.ErrorMessage = arg.ErrorMessage
	r.CompletedAt = now()
	f.storeRepositorySyncRun(r)
	return r, nil
}

// GetRepositorySyncRunByID mirrors the SQL: unscoped, for the background worker.
func (f *FakeQuerier) GetRepositorySyncRunByID(_ context.Context, id uuid.UUID) (db.RepositorySyncRun, error) {
	r, ok := f.repositorySyncRunsByID[id]
	if !ok {
		return db.RepositorySyncRun{}, pgx.ErrNoRows
	}
	return r, nil
}

// SeedProjectMember inserts a ready-made project_members row directly,
// bypassing project.Service. Use it in tests that need a specific
// non-owner role (viewer/member) on a project; an owner-role row is already
// seeded automatically by SeedProject. Returns the stored row.
func (f *FakeQuerier) SeedProjectMember(projectID, userID uuid.UUID, role string) db.ProjectMember {
	m := db.ProjectMember{ProjectID: projectID, UserID: userID, Role: role, CreatedAt: now()}
	f.projectMembers[projectMemberKey{projectID, userID}] = m
	return m
}

// AddProjectMember mirrors the SQL, including the (project_id, user_id)
// primary key's unique violation on a duplicate insert.
func (f *FakeQuerier) AddProjectMember(_ context.Context, arg db.AddProjectMemberParams) (db.ProjectMember, error) {
	key := projectMemberKey{arg.ProjectID, arg.UserID}
	if _, ok := f.projectMembers[key]; ok {
		return db.ProjectMember{}, &pgconn.PgError{Code: "23505", ConstraintName: "project_members_pkey"}
	}
	m := db.ProjectMember{ProjectID: arg.ProjectID, UserID: arg.UserID, Role: arg.Role, CreatedAt: now()}
	f.projectMembers[key] = m
	return m, nil
}

// ListProjectMembers mirrors the SQL's ORDER BY created_at ASC.
func (f *FakeQuerier) ListProjectMembers(_ context.Context, projectID uuid.UUID) ([]db.ProjectMember, error) {
	items := []db.ProjectMember{}
	for _, m := range f.projectMembers {
		if m.ProjectID == projectID {
			items = append(items, m)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Time.Before(items[j].CreatedAt.Time)
	})
	return items, nil
}

// SearchProjectMemberCandidates mirrors the invite form's candidate query:
// users sharing at least one project with the caller, minus the caller and
// minus the project's existing members, matched case-insensitively against
// the LIKE pattern the service builds (approximated as a substring match, the
// same shortcut matchesTaskQuery takes), username-ordered and capped at 10.
func (f *FakeQuerier) SearchProjectMemberCandidates(_ context.Context, arg db.SearchProjectMemberCandidatesParams) ([]db.SearchProjectMemberCandidatesRow, error) {
	shared := map[uuid.UUID]bool{}   // projects the caller belongs to
	existing := map[uuid.UUID]bool{} // users already in the target project
	for _, m := range f.projectMembers {
		if m.UserID == arg.CallerUserID {
			shared[m.ProjectID] = true
		}
		if m.ProjectID == arg.ProjectID {
			existing[m.UserID] = true
		}
	}

	term := strings.ToLower(unescapeLikePattern(arg.Query))
	seen := map[uuid.UUID]bool{}
	items := []db.SearchProjectMemberCandidatesRow{}
	for _, m := range f.projectMembers {
		if !shared[m.ProjectID] || m.UserID == arg.CallerUserID || existing[m.UserID] || seen[m.UserID] {
			continue
		}
		u := f.usersByID[m.UserID]
		if !strings.Contains(strings.ToLower(u.Username), term) &&
			!strings.Contains(strings.ToLower(u.DisplayName), term) {
			continue
		}
		seen[m.UserID] = true
		items = append(items, db.SearchProjectMemberCandidatesRow{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Username < items[j].Username })
	if len(items) > 10 {
		items = items[:10]
	}
	return items, nil
}

// unescapeLikePattern turns the "%term%" pattern the service builds back into
// the term the fake matches on, undoing its wildcard escaping.
func unescapeLikePattern(pattern string) string {
	term := strings.TrimSuffix(strings.TrimPrefix(pattern, "%"), "%")
	return strings.NewReplacer(`\%`, `%`, `\_`, `_`, `\\`, `\`).Replace(term)
}

// ListProjectMembersWithUser mirrors the SQL's join against users, ordered
// by created_at ASC.
func (f *FakeQuerier) ListProjectMembersWithUser(_ context.Context, projectID uuid.UUID) ([]db.ListProjectMembersWithUserRow, error) {
	items := []db.ListProjectMembersWithUserRow{}
	for _, m := range f.projectMembers {
		if m.ProjectID != projectID {
			continue
		}
		u := f.usersByID[m.UserID]
		items = append(items, db.ListProjectMembersWithUserRow{
			ProjectID:   m.ProjectID,
			UserID:      m.UserID,
			Role:        m.Role,
			CreatedAt:   m.CreatedAt,
			Username:    u.Username,
			DisplayName: u.DisplayName,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Time.Before(items[j].CreatedAt.Time)
	})
	return items, nil
}

// GetProjectMemberRole mirrors the SQL.
func (f *FakeQuerier) GetProjectMemberRole(_ context.Context, arg db.GetProjectMemberRoleParams) (string, error) {
	m, ok := f.projectMembers[projectMemberKey{arg.ProjectID, arg.UserID}]
	if !ok {
		return "", pgx.ErrNoRows
	}
	return m.Role, nil
}

// UpdateProjectMemberRole mirrors the SQL.
func (f *FakeQuerier) UpdateProjectMemberRole(_ context.Context, arg db.UpdateProjectMemberRoleParams) (db.ProjectMember, error) {
	key := projectMemberKey{arg.ProjectID, arg.UserID}
	m, ok := f.projectMembers[key]
	if !ok {
		return db.ProjectMember{}, pgx.ErrNoRows
	}
	m.Role = arg.Role
	f.projectMembers[key] = m
	return m, nil
}

// RemoveProjectMember mirrors the SQL.
func (f *FakeQuerier) RemoveProjectMember(_ context.Context, arg db.RemoveProjectMemberParams) (int64, error) {
	key := projectMemberKey{arg.ProjectID, arg.UserID}
	if _, ok := f.projectMembers[key]; !ok {
		return 0, nil
	}
	delete(f.projectMembers, key)
	return 1, nil
}

// UpsertNotificationSettings mirrors the SQL's ON CONFLICT (project_id) DO
// UPDATE.
func (f *FakeQuerier) UpsertNotificationSettings(_ context.Context, arg db.UpsertNotificationSettingsParams) (db.NotificationSetting, error) {
	existing, ok := f.notificationSettingsByProjectID[arg.ProjectID]
	s := db.NotificationSetting{
		ProjectID:  arg.ProjectID,
		WebhookUrl: arg.WebhookUrl,
		Enabled:    arg.Enabled,
		SendHour:   arg.SendHour,
		CreatedAt:  now(),
		UpdatedAt:  now(),
	}
	if ok {
		s.CreatedAt = existing.CreatedAt
	}
	f.notificationSettingsByProjectID[arg.ProjectID] = s
	return s, nil
}

// GetNotificationSettingsForOwner mirrors the SQL: a project with no
// settings row, or one belonging to a non-owner, is reported as
// pgx.ErrNoRows.
func (f *FakeQuerier) GetNotificationSettingsForOwner(_ context.Context, arg db.GetNotificationSettingsForOwnerParams) (db.NotificationSetting, error) {
	s, ok := f.notificationSettingsByProjectID[arg.ProjectID]
	if !ok || !f.hasRoleAtLeast(arg.ProjectID, arg.OwnerUserID, "owner") {
		return db.NotificationSetting{}, pgx.ErrNoRows
	}
	return s, nil
}

// ListEnabledNotificationSettings mirrors the SQL's unscoped scan.
func (f *FakeQuerier) ListEnabledNotificationSettings(_ context.Context) ([]db.NotificationSetting, error) {
	out := []db.NotificationSetting{}
	for _, s := range f.notificationSettingsByProjectID {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

// InsertNotificationDigestLog mirrors the SQL's ON CONFLICT (project_id,
// digest_date) DO NOTHING RETURNING: a colliding row reports pgx.ErrNoRows,
// the dedupe signal the worker checks for.
func (f *FakeQuerier) InsertNotificationDigestLog(_ context.Context, arg db.InsertNotificationDigestLogParams) (db.NotificationDigest, error) {
	key := notificationDigestKey{arg.ProjectID, arg.DigestDate.Time.Format("2006-01-02")}
	if _, ok := f.notificationDigests[key]; ok {
		return db.NotificationDigest{}, pgx.ErrNoRows
	}
	d := db.NotificationDigest{
		ID:         uuid.New(),
		ProjectID:  arg.ProjectID,
		DigestDate: arg.DigestDate,
		Status:     arg.Status,
		Error:      arg.Error,
		SentAt:     now(),
	}
	f.notificationDigests[key] = d
	return d, nil
}

// MarkNotificationDigestFailed mirrors the SQL.
func (f *FakeQuerier) MarkNotificationDigestFailed(_ context.Context, arg db.MarkNotificationDigestFailedParams) error {
	for key, d := range f.notificationDigests {
		if d.ID == arg.ID {
			d.Status = "failed"
			d.Error = arg.Error
			f.notificationDigests[key] = d
			return nil
		}
	}
	return nil
}

// ListOverdueOpenTasksByProject mirrors the SQL: open tasks whose due_on is
// set and strictly before today.
func (f *FakeQuerier) ListOverdueOpenTasksByProject(_ context.Context, arg db.ListOverdueOpenTasksByProjectParams) ([]db.Task, error) {
	out := []db.Task{}
	for _, t := range f.tasks {
		if t.ProjectID == arg.ProjectID && t.Status == "open" && t.DueOn.Valid && t.DueOn.Time.Before(arg.Today.Time) {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListTasksDueSoonByProject mirrors the SQL: open tasks due exactly today.
func (f *FakeQuerier) ListTasksDueSoonByProject(_ context.Context, arg db.ListTasksDueSoonByProjectParams) ([]db.Task, error) {
	out := []db.Task{}
	for _, t := range f.tasks {
		if t.ProjectID == arg.ProjectID && t.Status == "open" && dueOnEqual(t.DueOn, arg.Today) {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListFailedSyncJobsByProject mirrors the SQL: every failed job for the
// project, unscoped by caller.
func (f *FakeQuerier) ListFailedSyncJobsByProject(_ context.Context, projectID uuid.UUID) ([]db.SyncJob, error) {
	out := []db.SyncJob{}
	for _, j := range f.syncJobs {
		if j.ProjectID == projectID && j.Status == "failed" {
			out = append(out, j)
		}
	}
	return out, nil
}

// ListFailedWebhookEventsByProject mirrors the SQL's join through
// linked_gitlab_projects -> gitlab_connections.
func (f *FakeQuerier) ListFailedWebhookEventsByProject(_ context.Context, projectID uuid.UUID) ([]db.WebhookEvent, error) {
	out := []db.WebhookEvent{}
	for _, e := range f.webhookEvents {
		if e.Status != "failed" {
			continue
		}
		link, ok := f.linkedGitlabProjectsByID[e.LinkedGitlabProjectID]
		if !ok {
			continue
		}
		conn, ok := f.gitlabConnectionsByID[link.GitlabConnectionID]
		if !ok || conn.ProjectID != projectID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
