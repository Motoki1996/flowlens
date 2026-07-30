// Package dbtest provides an in-memory db.Querier implementation for unit
// tests, so use cases can be tested without a real PostgreSQL instance.
package dbtest

import (
	"context"
	"sort"
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

		gitlabConnectionsByProjectID: map[uuid.UUID]db.GitlabConnection{},
		gitlabConnectionsByID:        map[uuid.UUID]db.GitlabConnection{},

		linkedGitlabProjectsByID: map[uuid.UUID]db.LinkedGitlabProject{},

		syncJobsByID:        map[uuid.UUID]db.SyncJob{},
		syncJobsByDedupeKey: map[string]uuid.UUID{},

		taskGitlabLinksByTaskID: map[uuid.UUID]db.TaskGitlabLink{},

		webhookEventsByKey: map[string]db.WebhookEvent{},
		webhookEventsByID:  map[uuid.UUID]db.WebhookEvent{},

		gitlabSyncRunsByID: map[uuid.UUID]db.GitlabSyncRun{},
	}
}

func now() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
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
		ID:        uuid.New(),
		ProjectID: arg.ProjectID,
		Name:      arg.Name,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		CreatedAt: now(),
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
func (f *FakeQuerier) GetProjectAPITokenByTokenHash(_ context.Context, tokenHash string) (db.ProjectApiToken, error) {
	id, ok := f.projectAPITokensByHash[tokenHash]
	if !ok {
		return db.ProjectApiToken{}, pgx.ErrNoRows
	}
	t := f.projectAPITokensByID[id]
	if t.ExpiresAt.Valid && !t.ExpiresAt.Time.After(time.Now()) {
		return db.ProjectApiToken{}, pgx.ErrNoRows
	}
	return t, nil
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

// GetProjectForOwner mirrors the SQL: a project owned by someone else is
// reported as missing, never as a distinct "forbidden" outcome.
func (f *FakeQuerier) GetProjectForOwner(_ context.Context, arg db.GetProjectForOwnerParams) (db.Project, error) {
	p, ok := f.projectsByID[arg.ID]
	if !ok || p.OwnerUserID != arg.OwnerUserID {
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

func (f *FakeQuerier) ListProjectsByOwner(_ context.Context, ownerUserID uuid.UUID) ([]db.Project, error) {
	items := []db.Project{}
	for i := len(f.projects) - 1; i >= 0; i-- {
		if p := f.projects[i]; p.OwnerUserID == ownerUserID {
			items = append(items, p)
		}
	}
	return items, nil
}

func (f *FakeQuerier) UpdateProjectForOwner(_ context.Context, arg db.UpdateProjectForOwnerParams) (db.Project, error) {
	existing, ok := f.projectsByID[arg.ID]
	if !ok || existing.OwnerUserID != arg.OwnerUserID {
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
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	f.backlogs = append(f.backlogs, b)
	f.backlogsByID[b.ID] = b
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
		ID:          uuid.New(),
		ProjectID:   arg.ProjectID,
		Name:        arg.Name,
		Description: arg.Description,
		Position:    f.nextBacklogPosition(arg.ProjectID),
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}
	f.backlogs = append(f.backlogs, b)
	f.backlogsByID[b.ID] = b
	return b, nil
}

func (f *FakeQuerier) ListBacklogsByProject(_ context.Context, projectID uuid.UUID) ([]db.Backlog, error) {
	items := []db.Backlog{}
	for _, b := range f.backlogs {
		if b.ProjectID == projectID {
			items = append(items, b)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	return items, nil
}

// backlogOwner returns the owner_user_id of the project a backlog belongs
// to, mirroring the JOIN the real query performs.
func (f *FakeQuerier) backlogOwner(b db.Backlog) (uuid.UUID, bool) {
	p, ok := f.projectsByID[b.ProjectID]
	if !ok {
		return uuid.Nil, false
	}
	return p.OwnerUserID, true
}

// GetBacklogForOwner mirrors the SQL: a backlog whose project belongs to
// someone else is reported as missing, never as a distinct "forbidden"
// outcome.
func (f *FakeQuerier) GetBacklogForOwner(_ context.Context, arg db.GetBacklogForOwnerParams) (db.Backlog, error) {
	b, ok := f.backlogsByID[arg.ID]
	if !ok {
		return db.Backlog{}, pgx.ErrNoRows
	}
	owner, ok := f.backlogOwner(b)
	if !ok || owner != arg.OwnerUserID {
		return db.Backlog{}, pgx.ErrNoRows
	}
	return b, nil
}

func (f *FakeQuerier) UpdateBacklogForOwner(_ context.Context, arg db.UpdateBacklogForOwnerParams) (db.Backlog, error) {
	existing, ok := f.backlogsByID[arg.ID]
	if !ok {
		return db.Backlog{}, pgx.ErrNoRows
	}
	owner, ok := f.backlogOwner(existing)
	if !ok || owner != arg.OwnerUserID {
		return db.Backlog{}, pgx.ErrNoRows
	}

	existing.Name = arg.Name
	existing.Description = arg.Description
	existing.Position = arg.Position
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

// DeleteBacklogForOwner returns the number of rows affected, so callers can
// tell "deleted" from "not yours / not there" exactly as Postgres does.
func (f *FakeQuerier) DeleteBacklogForOwner(_ context.Context, arg db.DeleteBacklogForOwnerParams) (int64, error) {
	b, ok := f.backlogsByID[arg.ID]
	if !ok {
		return 0, nil
	}
	owner, ok := f.backlogOwner(b)
	if !ok || owner != arg.OwnerUserID {
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

func (f *FakeQuerier) seedTask(projectID, createdByUserID uuid.UUID, title string, backlogID pgtype.UUID) db.Task {
	t := db.Task{
		ID:              uuid.New(),
		ProjectID:       projectID,
		BacklogID:       backlogID,
		Title:           title,
		Status:          "open",
		Labels:          []string{},
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
		items = append(items, t)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
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

// taskOwner returns the owner_user_id of the project a task belongs to,
// mirroring the JOIN the real query performs.
func (f *FakeQuerier) taskOwner(t db.Task) (uuid.UUID, bool) {
	p, ok := f.projectsByID[t.ProjectID]
	if !ok {
		return uuid.Nil, false
	}
	return p.OwnerUserID, true
}

// GetTaskForOwner mirrors the SQL: a task whose project belongs to someone
// else is reported as missing, never as a distinct "forbidden" outcome.
func (f *FakeQuerier) GetTaskForOwner(_ context.Context, arg db.GetTaskForOwnerParams) (db.Task, error) {
	t, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	owner, ok := f.taskOwner(t)
	if !ok || owner != arg.OwnerUserID {
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

// CountFailedSyncTasksByProjectForOwner mirrors the SQL: a task counts as
// failed the same way internal/task derives a single task's sync_status,
// from task_gitlab_links when a link exists or from its most recent sync_jobs
// row (necessarily issue.create) when it doesn't.
func (f *FakeQuerier) CountFailedSyncTasksByProjectForOwner(_ context.Context, arg db.CountFailedSyncTasksByProjectForOwnerParams) (int64, error) {
	p, ok := f.projectsByID[arg.ProjectID]
	if !ok || p.OwnerUserID != arg.OwnerUserID {
		return 0, nil
	}

	var count int64
	for _, t := range f.tasks {
		if t.ProjectID != arg.ProjectID {
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
	return count, nil
}

func (f *FakeQuerier) UpdateTaskForOwner(_ context.Context, arg db.UpdateTaskForOwnerParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	owner, ok := f.taskOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.taskOwner(existing)
	if !ok || owner != arg.OwnerUserID {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.BacklogID = arg.BacklogID
	existing.UpdatedAt = now()
	f.storeTask(existing)
	return existing, nil
}

func (f *FakeQuerier) CloseTaskForOwner(_ context.Context, arg db.CloseTaskForOwnerParams) (db.Task, error) {
	existing, ok := f.tasksByID[arg.ID]
	if !ok {
		return db.Task{}, pgx.ErrNoRows
	}
	owner, ok := f.taskOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.taskOwner(existing)
	if !ok || owner != arg.OwnerUserID {
		return db.Task{}, pgx.ErrNoRows
	}
	existing.Status = "open"
	existing.ClosedAt = pgtype.Timestamptz{}
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
	owner, ok := f.taskOwner(t)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.taskOwner(t)
	if !ok || owner != arg.OwnerUserID {
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

// gitlabConnectionOwner returns the owner_user_id of the project a GitLab
// connection belongs to, mirroring the JOIN the real query performs.
func (f *FakeQuerier) gitlabConnectionOwner(c db.GitlabConnection) (uuid.UUID, bool) {
	p, ok := f.projectsByID[c.ProjectID]
	if !ok {
		return uuid.Nil, false
	}
	return p.OwnerUserID, true
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
	if !ok {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	owner, ok := f.gitlabConnectionOwner(c)
	if !ok || owner != arg.OwnerUserID {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	return c, nil
}

// GetGitlabConnectionByIDForOwner mirrors the SQL: same lookup as
// GetGitlabConnectionForOwner, but keyed by the connection's own ID.
func (f *FakeQuerier) GetGitlabConnectionByIDForOwner(_ context.Context, arg db.GetGitlabConnectionByIDForOwnerParams) (db.GitlabConnection, error) {
	c, ok := f.gitlabConnectionsByID[arg.ID]
	if !ok {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	owner, ok := f.gitlabConnectionOwner(c)
	if !ok || owner != arg.OwnerUserID {
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
	if !ok {
		return db.GitlabConnection{}, pgx.ErrNoRows
	}
	owner, ok := f.gitlabConnectionOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
	if !ok {
		return 0, nil
	}
	owner, ok := f.gitlabConnectionOwner(c)
	if !ok || owner != arg.OwnerUserID {
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

// linkedProjectOwner returns the owner_user_id of the project a linked
// GitLab project belongs to (through its connection), mirroring the JOIN
// chain the real queries perform.
func (f *FakeQuerier) linkedProjectOwner(l db.LinkedGitlabProject) (uuid.UUID, bool) {
	conn, ok := f.gitlabConnectionsByID[l.GitlabConnectionID]
	if !ok {
		return uuid.Nil, false
	}
	p, ok := f.projectsByID[conn.ProjectID]
	if !ok {
		return uuid.Nil, false
	}
	return p.OwnerUserID, true
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
		if !ok || conn.ProjectID != arg.ID {
			continue
		}
		p, ok := f.projectsByID[conn.ProjectID]
		if !ok || p.OwnerUserID != arg.OwnerUserID {
			continue
		}
		items = append(items, l)
	}
	return items, nil
}

// GetLinkedGitlabProjectForOwner mirrors the SQL: a link whose project
// belongs to someone else is reported as missing, never as a distinct
// "forbidden" outcome.
func (f *FakeQuerier) GetLinkedGitlabProjectForOwner(_ context.Context, arg db.GetLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	l, ok := f.linkedGitlabProjectsByID[arg.ID]
	if !ok {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	owner, ok := f.linkedProjectOwner(l)
	if !ok || owner != arg.OwnerUserID {
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

// GetDefaultLinkedGitlabProjectForOwner mirrors the SQL: the project's
// default link, scoped through its connection to the owning project like
// every other linked-project query.
func (f *FakeQuerier) GetDefaultLinkedGitlabProjectForOwner(_ context.Context, arg db.GetDefaultLinkedGitlabProjectForOwnerParams) (db.LinkedGitlabProject, error) {
	for _, l := range f.linkedGitlabProjects {
		if !l.IsDefault {
			continue
		}
		conn, ok := f.gitlabConnectionsByID[l.GitlabConnectionID]
		if !ok || conn.ProjectID != arg.ID {
			continue
		}
		p, ok := f.projectsByID[conn.ProjectID]
		if !ok || p.OwnerUserID != arg.OwnerUserID {
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
	owner, ok := f.linkedProjectOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.linkedProjectOwner(target)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.linkedProjectOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.linkedProjectOwner(existing)
	if !ok || owner != arg.OwnerUserID {
		return db.LinkedGitlabProject{}, pgx.ErrNoRows
	}
	delete(f.linkedGitlabProjectsByID, existing.ID)
	for i, x := range f.linkedGitlabProjects {
		if x.ID == existing.ID {
			f.linkedGitlabProjects = append(f.linkedGitlabProjects[:i], f.linkedGitlabProjects[i+1:]...)
			break
		}
	}
	return existing, nil
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
	owner, ok := f.linkedProjectOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
	owner, ok := f.linkedProjectOwner(existing)
	if !ok || owner != arg.OwnerUserID {
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
// specific state (e.g. a stale 'running' job for reclaim tests) without
// exercising the transition that would normally produce it.
func (f *FakeQuerier) SeedSyncJob(projectID uuid.UUID, kind, status string, updatedAt time.Time) db.SyncJob {
	j := db.SyncJob{
		ID:        uuid.New(),
		ProjectID: projectID,
		Kind:      kind,
		Payload:   []byte("{}"),
		Status:    status,
		RunAfter:  now(),
		CreatedAt: now(),
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
	l.GitlabUpdatedAt = arg.GitlabUpdatedAt
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
