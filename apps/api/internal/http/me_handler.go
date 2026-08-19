package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/user"
)

// handleMe returns the authenticated user's profile.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleChangePassword replaces the caller's own password (issue #210).
// Until this existed, a forgotten or shared password could only be fixed by
// an operator editing the database by hand.
//
// It is registered in the session-only group, never on the bearer-token
// allowlist: a project API token must not be able to take over the account
// it acts as, the same reasoning that keeps /api-tokens and member
// management off that list (see Router).
//
// Every session the user holds is revoked on success, including the one
// making the call, and a fresh session is issued in its place. Changing a
// password is what someone does when they think a session of theirs is in
// the wrong hands, so leaving any older token usable would defeat the
// point; issuing a new one keeps the caller logged in regardless.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	// Keyed by user rather than by IP like the login limiter: the caller is
	// already authenticated, so their ID is the stable identity here, and a
	// UUID can never collide with an IP key in the shared limiter.
	if !s.authLimiter.Allow("password:" + u.ID.String()) {
		writeTooManyRequests(w, authRateLimitWindow)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	ctx := r.Context()
	if err := s.users.ChangePassword(ctx, u.ID, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, user.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "current password is incorrect")
		case errors.Is(err, user.ErrPasswordTooShort):
			writeError(w, http.StatusBadRequest, "password_too_short", "password must be at least 8 characters")
		case errors.Is(err, user.ErrNotFound):
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		default:
			slog.Error("change password", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}

	// The password is already changed at this point, so a failure below is
	// reported but must not read as "nothing happened": the caller is left
	// logged out with the new password in place, which re-login resolves.
	if err := s.sessions.RevokeAll(ctx, u.ID); err != nil {
		slog.Error("revoke sessions after password change", "error", err)
		s.cookies.clearSession(w)
		s.cookies.clearCSRF(w)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		slog.Error("create session after password change", "error", err)
		s.cookies.clearSession(w)
		s.cookies.clearCSRF(w)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
