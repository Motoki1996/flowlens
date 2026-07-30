package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/google/uuid"
)

const tokenProjectContextKey contextKey = "flowlens_token_project"

// bearerToken extracts the raw token from an "Authorization: Bearer <token>"
// header. It reports false for a missing header, a non-Bearer scheme, or an
// empty token.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(header, prefix)
	if token == "" {
		return "", false
	}
	return token, true
}

// tokenProjectFromContext returns the project ID a bearer token
// authenticated for, set by requireBearerAuth. It is never set alongside
// userFromContext's value — a request is authenticated by exactly one of
// session or bearer token.
func tokenProjectFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(tokenProjectContextKey).(uuid.UUID)
	return id, ok
}

// requireBearerAuth resolves an `Authorization: Bearer <token>` header to
// the project.Project it was issued for (internal/apitoken, project-scoped
// API tokens per docs/plans/issue-sync.md "AI-facing"), and rejects the
// request with 401 when the token is missing, unknown, expired, or revoked
// — the three failure cases are never distinguished in the response.
// Unlike requireAuth, it never resolves a user: a bearer caller is scoped to
// its token's project only, put in context for requireTokenProjectMatch (or
// a handler) to check against.
func (s *Server) requireBearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		projectID, err := s.apiTokens.Authenticate(r.Context(), token)
		if err != nil {
			if errors.Is(err, apitoken.ErrTokenNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			slog.Error("authenticate api token", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		ctx := context.WithValue(r.Context(), tokenProjectContextKey, projectID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuthOrBearer accepts either the session cookie or an
// `Authorization: Bearer <project token>` header, composed from requireAuth
// and requireBearerAuth rather than duplicating either's logic. A request
// carrying a session cookie is always treated as session auth (an invalid
// cookie is a 401, it never falls back to bearer); only a request with no
// cookie at all tries bearer auth. This is reserved for future project-scoped,
// AI-facing endpoints (docs/plans/issue-sync.md) that must serve both a
// logged-in user and an external integration — no route uses it yet, the
// same way internal/gitlab.Client existed before any handler used it.
func (s *Server) requireAuthOrBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(sessionCookieName); err == nil {
			s.requireAuth(next).ServeHTTP(w, r)
			return
		}
		s.requireBearerAuth(next).ServeHTTP(w, r)
	})
}

// requireTokenProjectMatch enforces that a bearer-authenticated request only
// reaches resources under its own token's project: a {projectID} URL
// parameter that does not match the token's project is reported as 404,
// identical to how project-scoped session handlers treat a foreign project
// ID (never 403, so a token can't distinguish "not yours" from "does not
// exist"). It is a no-op for session-authenticated requests, which enforce
// ownership themselves via project.Service.
func requireTokenProjectMatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tokenProjectID, ok := tokenProjectFromContext(r.Context()); ok {
			urlProjectID, ok := projectIDFromURL(r)
			if !ok || urlProjectID != tokenProjectID {
				writeError(w, http.StatusNotFound, "not_found", "project not found")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
