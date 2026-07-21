// Package user contains the user domain model and the service that
// persists users coming from GitHub. It keeps database types (pgtype)
// from leaking into HTTP responses.
package user

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/github"
)

// User is the API-facing representation of a FlowLens user.
type User struct {
	ID           string `json:"id"`
	GitHubUserID int64  `json:"githubUserId"`
	GitHubLogin  string `json:"githubLogin"`
	DisplayName  string `json:"displayName"`
	AvatarURL    string `json:"avatarUrl"`
}

// FromDB maps a database row to the domain model, never exposing the
// encrypted access token.
func FromDB(row db.User) User {
	return User{
		ID:           hex.EncodeToString(row.ID.Bytes[:]),
		GitHubUserID: row.GithubUserID,
		GitHubLogin:  row.GithubLogin,
		DisplayName:  row.DisplayName,
		AvatarURL:    row.AvatarUrl,
	}
}

// Service persists users authenticated through GitHub.
type Service struct {
	q      db.Querier
	cipher auth.TokenCipher
}

// NewService constructs a user Service.
func NewService(q db.Querier, cipher auth.TokenCipher) *Service {
	return &Service{q: q, cipher: cipher}
}

// UpsertFromGitHub encrypts the access token and upserts the user by
// GitHub user ID, making repeated logins idempotent.
func (s *Service) UpsertFromGitHub(ctx context.Context, ghUser *github.User, accessToken string) (db.User, error) {
	encrypted, err := s.cipher.Encrypt(accessToken)
	if err != nil {
		return db.User{}, fmt.Errorf("user: encrypt token: %w", err)
	}
	row, err := s.q.UpsertUser(ctx, db.UpsertUserParams{
		GithubUserID:         ghUser.ID,
		GithubLogin:          ghUser.Login,
		DisplayName:          ghUser.Name,
		AvatarUrl:            ghUser.AvatarURL,
		EncryptedAccessToken: encrypted,
	})
	if err != nil {
		return db.User{}, fmt.Errorf("user: upsert: %w", err)
	}
	return row, nil
}
