package http

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/auth"
)

// handleGitHubLogin starts the OAuth flow: it stores an anti-CSRF state in
// a short-lived cookie and redirects the browser to GitHub.
func (s *Server) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	if s.oauth.ClientID == "" {
		writeError(w, http.StatusServiceUnavailable, "oauth_not_configured",
			"GitHub OAuth is not configured")
		return
	}
	state, err := auth.NewState()
	if err != nil {
		slog.Error("generate oauth state", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.cookies.setState(w, state)
	http.Redirect(w, r, s.oauth.AuthCodeURL(state), http.StatusTemporaryRedirect)
}

// handleGitHubCallback completes the OAuth flow: it verifies state,
// exchanges the code for a token, upserts the user and issues a session.
func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Verify the state parameter against the state cookie (CSRF defence).
	stateCookie, err := r.Cookie(stateCookieName)
	s.cookies.clearState(w)
	queryState := r.URL.Query().Get("state")
	if err != nil || queryState == "" ||
		subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(queryState)) != 1 {
		s.redirectToWebError(w, r, "invalid_state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectToWebError(w, r, "missing_code")
		return
	}

	token, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		slog.Error("oauth token exchange", "error", err)
		s.redirectToWebError(w, r, "exchange_failed")
		return
	}

	ghUser, err := s.github.GetAuthenticatedUser(ctx, token.AccessToken)
	if err != nil {
		slog.Error("fetch github user", "error", err)
		s.redirectToWebError(w, r, "github_user_failed")
		return
	}

	row, err := s.users.UpsertFromGitHub(ctx, ghUser, token.AccessToken)
	if err != nil {
		slog.Error("upsert user", "error", err)
		s.redirectToWebError(w, r, "user_persist_failed")
		return
	}

	sessionToken, err := s.sessions.Create(ctx, row.ID)
	if err != nil {
		slog.Error("create session", "error", err)
		s.redirectToWebError(w, r, "session_failed")
		return
	}

	s.cookies.setSession(w, sessionToken, int(s.sessionTTL.Seconds()))
	http.Redirect(w, r, s.webBaseURL+"/dashboard", http.StatusTemporaryRedirect)
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

// redirectToWebError sends the browser back to the web login page with an
// error code it can display.
func (s *Server) redirectToWebError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, s.webBaseURL+"/login?error="+code, http.StatusTemporaryRedirect)
}
