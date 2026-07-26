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
