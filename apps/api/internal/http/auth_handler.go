package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/user"
)

// authRateLimit and authRateLimitWindow bound how many login/signup
// attempts a single client IP can make in a window (issue #91: unlimited
// attempts let password brute-forcing and account enumeration through).
// Keyed by clientIP like webhookLimiter, since a would-be attacker isn't
// authenticated yet and has no other stable identity to key on.
const (
	authRateLimit       = 10
	authRateLimitWindow = 15 * time.Minute
)

type signupRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// handleSignup creates a local account and starts a session for it.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.Allow(clientIP(r)) {
		writeTooManyRequests(w, authRateLimitWindow)
		return
	}

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}
	if req.Username == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "username and email are required")
		return
	}

	ctx := r.Context()
	u, err := s.users.SignUp(ctx, user.SignUpInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, user.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "username_taken", "username is already taken")
		case errors.Is(err, user.ErrEmailTaken):
			writeError(w, http.StatusConflict, "email_taken", "email is already registered")
		case errors.Is(err, user.ErrPasswordTooShort):
			writeError(w, http.StatusBadRequest, "password_too_short", "password must be at least 8 characters")
		default:
			slog.Error("sign up", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}

	if err := s.startSession(w, r, u.ID); err != nil {
		slog.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// handleLogin verifies credentials and starts a session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.Allow(clientIP(r)) {
		writeTooManyRequests(w, authRateLimitWindow)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	ctx := r.Context()
	u, err := s.users.Authenticate(ctx, req.Identifier, req.Password)
	if err != nil {
		if errors.Is(err, user.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username/email or password")
			return
		}
		slog.Error("authenticate", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if err := s.startSession(w, r, u.ID); err != nil {
		slog.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// handleLogout revokes the current session and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.sessions.Revoke(r.Context(), cookie.Value); err != nil {
			slog.Error("revoke session", "error", err)
		}
	}
	s.cookies.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}
