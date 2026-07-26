// Package dbtest provides an in-memory db.Querier implementation for unit
// tests, so use cases can be tested without a real PostgreSQL instance.
package dbtest

import (
	"context"
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
