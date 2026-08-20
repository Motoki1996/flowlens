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
	// InviteToken, when present and valid, both exempts this signup from
	// ALLOW_SIGNUP and joins the new account to the invite's project
	// (issue #211).
	InviteToken string `json:"inviteToken"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// handleSignup creates a local account and starts a session for it.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.Allow(s.clientIP(r)) {
		writeTooManyRequests(w, authRateLimitWindow)
		return
	}

	ctx := r.Context()

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	// An invite is checked here, before anything is created: an expired or
	// already-spent invite must fail while it can still fail cleanly, not
	// after an account exists on an instance that has closed registration
	// (issue #211).
	invited := req.InviteToken != ""
	if invited {
		if _, err := s.projectInvites.Preview(ctx, req.InviteToken); err != nil {
			writeProjectInviteError(w, err)
			return
		}
	}

	// ALLOW_SIGNUP=false closes registration on an instance whose accounts
	// already exist, so that reaching the login page is not enough to
	// create one. The first account is exempt: a fresh instance brought up
	// with signup already off would otherwise have no way in at all. A
	// valid invite is the other exemption — it names one project and is
	// good for one account, which is what lets an operator keep
	// registration closed and still onboard a teammate.
	if !s.allowSignup && !invited {
		count, err := s.users.Count(ctx)
		if err != nil {
			slog.Error("count users", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		if count > 0 {
			writeError(w, http.StatusForbidden, "signup_disabled", "signup is disabled on this instance")
			return
		}
	}

	if req.Username == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "username and email are required")
		return
	}

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

	// Spend the invite now that the account it names exists. Preview above
	// already rejected the invalid cases, so the only way this fails is
	// someone else spending the same single-use invite in the intervening
	// moment — which leaves an account with no project rather than a member
	// of the wrong one. Reporting it is the honest outcome: the account can
	// still be logged into, and a fresh invite fixes it.
	if invited {
		if _, err := s.projectInvites.Accept(ctx, req.InviteToken, u.ID); err != nil {
			slog.Error("accept invite during signup", "error", err)
			writeProjectInviteError(w, err)
			return
		}
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
	if !s.authLimiter.Allow(s.clientIP(r)) {
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
	s.cookies.clearCSRF(w)
	w.WriteHeader(http.StatusNoContent)
}
