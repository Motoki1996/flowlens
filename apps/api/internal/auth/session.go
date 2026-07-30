package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/user"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrSessionNotFound is returned when a session token is missing, expired
// or otherwise invalid.
var ErrSessionNotFound = errors.New("auth: session not found")

// SessionService issues and validates opaque, server-side sessions. The
// cookie carries a random token; only its SHA-256 hash is stored, so a
// database leak does not expose usable session tokens.
type SessionService struct {
	q   db.Querier
	ttl time.Duration
}

// NewSessionService constructs a SessionService.
func NewSessionService(q db.Querier, ttl time.Duration) *SessionService {
	return &SessionService{q: q, ttl: ttl}
}

// Create issues a new session for a user and returns the raw token that
// belongs in the cookie.
func (s *SessionService) Create(ctx context.Context, userID uuid.UUID) (string, error) {
	rawToken, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = s.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: hashToken(rawToken),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(s.ttl), Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("auth: create session: %w", err)
	}
	return rawToken, nil
}

// Authenticate resolves a raw session token to its user. The session and
// the user are fetched in one joined query, so callers get the domain
// user without a second round trip.
func (s *SessionService) Authenticate(ctx context.Context, rawToken string) (user.User, error) {
	if rawToken == "" {
		return user.User{}, ErrSessionNotFound
	}
	row, err := s.q.GetUserBySessionToken(ctx, hashToken(rawToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, ErrSessionNotFound
		}
		return user.User{}, fmt.Errorf("auth: authenticate: %w", err)
	}
	return user.FromRow(row.User), nil
}

// Revoke deletes a session by its raw token. Revoking an unknown token is
// not an error (logout is idempotent).
func (s *SessionService) Revoke(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if err := s.q.DeleteSessionByTokenHash(ctx, hashToken(rawToken)); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NewOpaqueToken returns a fresh random opaque token, suitable for any
// bearer-style credential (session cookie, project API token) that is
// stored server-side only as its HashToken hash.
func NewOpaqueToken() (string, error) {
	return randomToken()
}

// HashToken returns the SHA-256 hash of raw, for storage instead of the raw
// value it was derived from.
func HashToken(raw string) string {
	return hashToken(raw)
}
