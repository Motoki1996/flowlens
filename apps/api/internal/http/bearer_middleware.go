package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const tokenScopeContextKey contextKey = "flowlens_token_scope"

// tokenRateLimit and tokenRateLimitWindow bound how many requests a single
// API token can make in a window (issue #67: an unattended AI agent or a
// broken integration must not be able to hammer the API without limit).
// This is a separate budget from the webhook receiver's own
// webhookRateLimit/webhookRateLimitWindow, and is keyed by token ID rather
// than caller IP, since a single integration's requests should be bounded
// regardless of which address they come from.
const (
	tokenRateLimit       = 120
	tokenRateLimitWindow = time.Minute
)

// tokenScope is what a bearer-authenticated request carries in context
// alongside its resolved user (see requireBearerAuth): the project the token
// was issued for and the scopes it was granted. userFromContext alone is
// enough for the owner-scoped checks every service method already performs
// (issue #66's "existing handlers need no changes" design), but a token acts
// as its owner across *every* project that owner has — tokenScope is what
// lets HTTP-layer middleware (requireTokenScope,
// requireTokenProjectMatch/requireTokenResourceProject) add the second,
// project-boundary check a session request never needs.
type tokenScope struct {
	TokenID   uuid.UUID
	ProjectID uuid.UUID
	Scopes    []string
}

// HasScope reports whether ts's token was granted scope.
func (ts tokenScope) HasScope(scope string) bool {
	for _, s := range ts.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// tokenScopeFromContext returns the tokenScope set by requireBearerAuth. It
// is never set alongside a session-authenticated request — a request is
// authenticated by exactly one of session or bearer token, and only the
// latter has a scope-limited project to check against.
func tokenScopeFromContext(ctx context.Context) (tokenScope, bool) {
	ts, ok := ctx.Value(tokenScopeContextKey).(tokenScope)
	return ts, ok
}

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
// authenticated for — the ProjectID half of tokenScopeFromContext, kept as
// its own accessor since most callers (task_context_handler.go,
// requireTokenProjectMatch) only ever need the project, not the scopes.
func tokenProjectFromContext(ctx context.Context) (uuid.UUID, bool) {
	ts, ok := tokenScopeFromContext(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return ts.ProjectID, true
}

// requireBearerAuth resolves an `Authorization: Bearer <token>` header to
// the apitoken.Auth it grants, and rejects the request with 401 when the
// token is missing, unknown, expired, or revoked — the three failure cases
// are never distinguished in the response. A successfully authenticated
// request is then subject to s.tokenLimiter, keyed by the token's own ID,
// and rejected with 429 + Retry-After once its budget (tokenRateLimit per
// tokenRateLimitWindow) is exhausted — this applies only to
// bearer-authenticated requests, never to a session cookie.
//
// It resolves the token's project owner to a full user.User and puts it in
// userContextKey — the same key requireAuth uses — so every handler and
// service method that already scopes its work by userFromContext's ID needs
// no bearer-specific branch at all (issue #66): ownership is enforced
// identically whether the caller is a logged-in owner or their own token.
// That alone is not enough, though, since a token must act as its owner
// across only *one* of that owner's projects, not all of them — so the
// token's project and scopes also go into tokenScopeContextKey, for
// requireTokenScope and requireTokenProjectMatch/requireTokenResourceProject
// to enforce the project boundary a session request never needs.
func (s *Server) requireBearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		tokenAuth, err := s.apiTokens.Authenticate(r.Context(), token)
		if err != nil {
			if errors.Is(err, apitoken.ErrTokenNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			slog.Error("authenticate api token", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		if !s.tokenLimiter.Allow(tokenAuth.TokenID.String()) {
			writeTooManyRequests(w, tokenRateLimitWindow)
			return
		}
		owner, err := s.users.ByID(r.Context(), tokenAuth.OwnerUserID)
		if err != nil {
			slog.Error("resolve api token owner", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, owner)
		ctx = context.WithValue(ctx, tokenScopeContextKey, tokenScope{TokenID: tokenAuth.TokenID, ProjectID: tokenAuth.ProjectID, Scopes: tokenAuth.Scopes})
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

// requireTokenScope enforces that a bearer-authenticated request's token was
// granted scope, rejecting it with 403 forbidden otherwise. It is a no-op
// for session-authenticated requests, which have no notion of scope — a
// logged-in user can always do everything their session's routes allow.
// Server.Router applies this only to non-GET route groups, since every
// token is granted at least apitoken.ScopeRead (apitoken.normalizeScopes
// never issues an empty scope set).
func requireTokenScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ts, ok := tokenScopeFromContext(r.Context()); ok && !ts.HasScope(scope) {
				writeError(w, http.StatusForbidden, "forbidden", "token missing required scope")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireTokenResourceProject is requireTokenProjectMatch for a
// single-resource URL (e.g. {taskID}, {backlogID}, {dependencyID}) that
// carries no {projectID} of its own to compare directly: resolve first
// looks up the project the paramName resource belongs to (e.g.
// task.Service.ProjectID), then the same 404-on-mismatch rule applies. This
// is the check that keeps a token confined to its own project even though,
// per requireBearerAuth, it now acts as a real user who may own several
// projects — without it, a token could reach any resource in any project
// that same owner has, via the owner-scoped check every service method
// already performs.
//
// notFound is resolve's sentinel error for a resource that does not exist
// (e.g. task.ErrNotFound), compared with errors.Is so the mismatch and the
// not-exists cases collapse into the same 404, exactly like
// requireTokenProjectMatch and every ownership check elsewhere in this
// codebase: a token must never be able to tell "not yours" from "does not
// exist". Any other error is a genuine failure (e.g. the database is down)
// and is reported as 500. It is a no-op for session-authenticated requests.
func requireTokenResourceProject(paramName string, notFound error, resolve func(context.Context, uuid.UUID) (uuid.UUID, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ts, ok := tokenScopeFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			resourceID, err := uuid.Parse(chi.URLParam(r, paramName))
			if err != nil {
				writeError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			projectID, err := resolve(r.Context(), resourceID)
			if err != nil {
				if errors.Is(err, notFound) {
					writeError(w, http.StatusNotFound, "not_found", "not found")
					return
				}
				slog.Error("resolve token resource project", "error", err)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}
			if projectID != ts.ProjectID {
				writeError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
