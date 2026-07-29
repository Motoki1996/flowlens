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

	projects            []db.Project // insertion order, newest last
	projectsByID        map[uuid.UUID]db.Project
	projectsByOwnerName map[string]db.Project // key: owner_user_id + name

	backlogs     []db.Backlog // insertion order, newest last
	backlogsByID map[uuid.UUID]db.Backlog

	tasks     []db.Task // insertion order, newest last
	tasksByID map[uuid.UUID]db.Task

	taskAIContextsByTaskID map[uuid.UUID]db.TaskAiContext

	gitlabConnectionsByProjectID map[uuid.UUID]db.GitlabConnection
	gitlabConnectionsByID        map[uuid.UUID]db.GitlabConnection

	linkedGitlabProjects     []db.LinkedGitlabProject // insertion order, newest last
	linkedGitlabProjectsByID map[uuid.UUID]db.LinkedGitlabProject

	syncJobs            []db.SyncJob // insertion order, newest last
	syncJobsByID        map[uuid.UUID]db.SyncJob
	syncJobsByDedupeKey map[string]uuid.UUID

	taskGitlabLinksByTaskID map[uuid.UUID]db.TaskGitlabLink
}

// New returns an empty FakeQuerier.
func New() *FakeQuerier {
	return &FakeQuerier{
		usersByUsername:     map[string]db.User{},
		usersByEmail:        map[string]db.User{},
		usersByID:           map[uuid.UUID]db.User{},
		sessions:            map[string]db.Session{},
		projectsByID:        map[uuid.UUID]db.Project{},
		projectsByOwnerName: map[string]db.Project{},
		backlogsByID:        map[uuid.UUID]db.Backlog{},
		tasksByID:           map[uuid.UUID]db.Task{},

		taskAIContextsByTaskID: map[uuid.UUID]db.TaskAiContext{},

		gitlabConnectionsByProjectID: map[uuid.UUID]db.GitlabConnection{},
		gitlabConnectionsByID:        map[uuid.UUID]db.GitlabConnection{},

		linkedGitlabProjectsByID: map[uuid.UUID]db.LinkedGitlabProject{},

		syncJobsByID:        map[uuid.UUID]db.SyncJob{},
		syncJobsByDedupeKey: map[string]uuid.UUID{},

		taskGitlabLinksByTaskID: map[uuid.UUID]db.TaskGitlabLink{},
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
		Position:               f.nextTaskPosition(arg.ProjectID, arg.BacklogID),
		CreatedByUserID:        arg.CreatedByUserID,
		CreatedAt:              now(),
		UpdatedAt:              now(),
	}
	f.storeTask(t)
	return t, nil
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
