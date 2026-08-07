package http

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/user"
)

type contextKey string

const userContextKey contextKey = "flowlens_user"

// corsMiddleware allows the configured web origin to call the API with
// credentials (cookies).
//
// Access-Control-Allow-Headers deliberately never lists "Authorization"
// (issue #66): the web app only ever authenticates with the session cookie,
// never a bearer API token, so a browser has no legitimate reason to send
// one. Leaving it off means the browser's CORS preflight rejects any
// cross-origin script that tries to attach a token to a request, keeping
// bearer tokens usable only for direct, server-to-server calls and shrinking
// the token's exposure to XSS in the web app to zero.
func corsMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == allowedOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger logs each request with method, path, status and duration.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// requireAuth resolves the session cookie to a user and rejects the
// request with 401 when the session is missing or invalid.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		u, err := s.sessions.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrSessionNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			slog.Error("authenticate session", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// userFromContext returns the authenticated user set by requireAuth.
func userFromContext(ctx context.Context) (user.User, bool) {
	u, ok := ctx.Value(userContextKey).(user.User)
	return u, ok
}

// csrfHeaderName is the header a session-authenticated mutation must echo
// the csrfCookieName cookie's value in (issue #92's double-submit check).
const csrfHeaderName = "X-CSRF-Token"

// isSafeMethod reports whether method never mutates state and so is exempt
// from the CSRF check.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// requireCSRF enforces the double-submit CSRF check (issue #92) on
// session-authenticated mutations: the X-CSRF-Token header must match the
// csrfCookieName cookie set at login. A cross-site request can make the
// browser attach the cookie automatically, but has no way to read its value
// (no HttpOnly, so only same-origin script can see it) to also set the
// matching header.
//
// It is a no-op for safe methods and for bearer-authenticated requests
// (detected via tokenScopeFromContext, set only by requireBearerAuth): a
// bearer token is sent explicitly by the caller in an Authorization header,
// never attached automatically by a browser, so it carries no CSRF risk —
// per the issue, this must hold even when requireAuthOrBearer resolved the
// same request that requireAuth would have.
func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := tokenScopeFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		header := r.Header.Get(csrfHeaderName)
		if err != nil || header == "" || cookie.Value == "" ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_token_mismatch", "missing or invalid CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}
