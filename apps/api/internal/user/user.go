// Package user contains the user domain model and the service that
// authenticates users locally by username/email + password.
//
// Service accepts and returns only types declared here (User, uuid.UUID);
// database row types never cross the package boundary, so callers such as
// internal/http never import internal/database/db or pgtype.
package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
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
	ErrNotFound           = errors.New("user: not found")
)

// User is the API-facing representation of a FlowLens user. The password
// hash has no field here, so it cannot be serialised by accident.
type User struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
}

// FromRow maps a database row to the domain model. It is exported only so
// that other server-side packages holding a db.User row (auth, when it
// resolves a session) can produce a User; HTTP handlers never need it.
func FromRow(row db.User) User {
	return User{
		ID:          row.ID,
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
func (s *Service) SignUp(ctx context.Context, in SignUpInput) (User, error) {
	hash, err := hashPassword(in.Password)
	if err != nil {
		return User{}, err
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
				return User{}, ErrEmailTaken
			}
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("user: create: %w", err)
	}
	return FromRow(row), nil
}

// Authenticate verifies a username-or-email + password pair.
func (s *Service) Authenticate(ctx context.Context, identifier, password string) (User, error) {
	row, err := s.q.GetUserByUsernameOrEmail(ctx, identifier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("user: lookup: %w", err)
	}
	if err := verifyPassword(row.PasswordHash, password); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return FromRow(row), nil
}

// ByID returns one user by its ID.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (User, error) {
	row, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("user: get: %w", err)
	}
	return FromRow(row), nil
}
