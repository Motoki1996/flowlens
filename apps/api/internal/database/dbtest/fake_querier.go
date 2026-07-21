// Package dbtest provides an in-memory db.Querier implementation for unit
// tests, so use cases can be tested without a real PostgreSQL instance.
package dbtest

import (
	"context"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// FakeQuerier implements db.Querier backed by maps.
type FakeQuerier struct {
	usersByGitHubID map[int64]db.User
	usersByID       map[string]db.User
	sessions        map[string]db.Session // key: token_hash
}

// New returns an empty FakeQuerier.
func New() *FakeQuerier {
	return &FakeQuerier{
		usersByGitHubID: map[int64]db.User{},
		usersByID:       map[string]db.User{},
		sessions:        map[string]db.Session{},
	}
}

func newUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

func (f *FakeQuerier) UpsertUser(_ context.Context, arg db.UpsertUserParams) (db.User, error) {
	existing, ok := f.usersByGitHubID[arg.GithubUserID]
	id := newUUID()
	created := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if ok {
		id = existing.ID
		created = existing.CreatedAt
	}
	u := db.User{
		ID:                   id,
		GithubUserID:         arg.GithubUserID,
		GithubLogin:          arg.GithubLogin,
		DisplayName:          arg.DisplayName,
		AvatarUrl:            arg.AvatarUrl,
		EncryptedAccessToken: arg.EncryptedAccessToken,
		CreatedAt:            created,
		UpdatedAt:            pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.usersByGitHubID[arg.GithubUserID] = u
	f.usersByID[string(u.ID.Bytes[:])] = u
	return u, nil
}

func (f *FakeQuerier) GetUserByID(_ context.Context, id pgtype.UUID) (db.User, error) {
	u, ok := f.usersByID[string(id.Bytes[:])]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (f *FakeQuerier) CreateSession(_ context.Context, arg db.CreateSessionParams) (db.Session, error) {
	s := db.Session{
		ID:        newUUID(),
		UserID:    arg.UserID,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.sessions[arg.TokenHash] = s
	return s, nil
}

func (f *FakeQuerier) GetUserBySessionToken(_ context.Context, tokenHash string) (db.GetUserBySessionTokenRow, error) {
	s, ok := f.sessions[tokenHash]
	if !ok || !s.ExpiresAt.Time.After(time.Now()) {
		return db.GetUserBySessionTokenRow{}, pgx.ErrNoRows
	}
	u, ok := f.usersByID[string(s.UserID.Bytes[:])]
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
