// Package user contains the user domain model and the service that
// authenticates users locally by username/email + password. It keeps
// database types (pgtype) from leaking into HTTP responses.
package user

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes; ErrInvalidCredentials is used for both "not found" and "wrong
// password" so login responses don't reveal which one occurred.
var (
	ErrUsernameTaken      = errors.New("user: username already taken")
	ErrEmailTaken         = errors.New("user: email already taken")
	ErrInvalidCredentials = errors.New("user: invalid credentials")
)

// User is the API-facing representation of a FlowLens user.
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

// FromDB maps a database row to the domain model, never exposing the
// password hash.
func FromDB(row db.User) User {
	return User{
		ID:          hex.EncodeToString(row.ID.Bytes[:]),
		Username:    row.Username,
		Email:       row.Email,
		DisplayName: row.DisplayName,
	}
}

// Service authenticates users locally.
type Service struct {
	q db.Querier
}

// NewService constructs a user Service.
func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

// SignUpInput holds the fields required to create a local account.
type SignUpInput struct {
	Username    string
	Email       string
	Password    string
	DisplayName string
}

// SignUp hashes the password and creates a new local account.
func (s *Service) SignUp(ctx context.Context, in SignUpInput) (db.User, error) {
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return db.User{}, err
	}
	displayName := in.DisplayName
	if displayName == "" {
		displayName = in.Username
	}
	row, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Username:     in.Username,
		Email:        in.Email,
		DisplayName:  displayName,
		PasswordHash: hash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "users_email_key" {
				return db.User{}, ErrEmailTaken
			}
			return db.User{}, ErrUsernameTaken
		}
		return db.User{}, fmt.Errorf("user: create: %w", err)
	}
	return row, nil
}

// Authenticate verifies a username-or-email + password pair.
func (s *Service) Authenticate(ctx context.Context, identifier, password string) (db.User, error) {
	row, err := s.q.GetUserByUsernameOrEmail(ctx, identifier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, ErrInvalidCredentials
		}
		return db.User{}, fmt.Errorf("user: lookup: %w", err)
	}
	if err := auth.VerifyPassword(row.PasswordHash, password); err != nil {
		return db.User{}, ErrInvalidCredentials
	}
	return row, nil
}
