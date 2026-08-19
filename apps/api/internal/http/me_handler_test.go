package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signUpSession registers a real account through the signup handler and
// returns its session token. Unlike loginSession's seeded user, this one
// has a genuine bcrypt hash behind it — which the password-change handler
// verifies, so the seeded "seeded-hash" placeholder cannot be used here.
func signUpSession(t *testing.T, s *Server, password string) string {
	t.Helper()
	body, err := json.Marshal(signupRequest{Username: "tester", Email: "tester@example.com", Password: password})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	c := sessionCookie(rec)
	require.NotNil(t, c)
	return c.Value
}

func TestHandleChangePassword_ReplacesPasswordAndRotatesSessions(t *testing.T) {
	s, _ := newTestServer(t)
	token := signUpSession(t, s, "hunter22")

	// A second session for the same user, standing in for the browser
	// elsewhere that a password change is meant to cut off.
	otherToken := signUpSessionSecondDevice(t, s, "hunter22")

	rec := doRequest(t, s, http.MethodPut, "/api/v1/me/password", changePasswordRequest{
		CurrentPassword: "hunter22",
		NewPassword:     "correct-horse",
	}, token)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	// A fresh session cookie replaces the one that made the call, so the
	// caller stays logged in on a token that did not exist before.
	fresh := sessionCookie(rec)
	require.NotNil(t, fresh, "a new session cookie must be set")
	assert.NotEqual(t, token, fresh.Value)

	assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/me", nil, fresh.Value).Code)
	assert.Equal(t, http.StatusUnauthorized, doRequest(t, s, http.MethodGet, "/api/v1/me", nil, token).Code,
		"the session that changed the password must not survive it")
	assert.Equal(t, http.StatusUnauthorized, doRequest(t, s, http.MethodGet, "/api/v1/me", nil, otherToken).Code,
		"every other session must be revoked")

	// The new password is what logs in from here.
	assert.Equal(t, http.StatusOK, postJSON(t, s, "/auth/login", loginRequest{Identifier: "tester", Password: "correct-horse"}).Code)
	assert.Equal(t, http.StatusUnauthorized, postJSON(t, s, "/auth/login", loginRequest{Identifier: "tester", Password: "hunter22"}).Code)
}

// signUpSessionSecondDevice logs the already-registered user in again, for
// the "another session exists" half of the rotation test.
func signUpSessionSecondDevice(t *testing.T, s *Server, password string) string {
	t.Helper()
	rec := postJSON(t, s, "/auth/login", loginRequest{Identifier: "tester", Password: password})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	c := sessionCookie(rec)
	require.NotNil(t, c)
	return c.Value
}

func TestHandleChangePassword_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		body     changePasswordRequest
		wantCode int
		wantErr  string
	}{
		{
			name:     "wrong current password",
			body:     changePasswordRequest{CurrentPassword: "not-my-password", NewPassword: "correct-horse"},
			wantCode: http.StatusUnauthorized,
			wantErr:  "invalid_credentials",
		},
		{
			name:     "new password too short",
			body:     changePasswordRequest{CurrentPassword: "hunter22", NewPassword: "short"},
			wantCode: http.StatusBadRequest,
			wantErr:  "password_too_short",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			token := signUpSession(t, s, "hunter22")

			rec := doRequest(t, s, http.MethodPut, "/api/v1/me/password", tt.body, token)
			require.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			var body map[string]map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantErr, body["error"]["code"])

			// A rejected change leaves the session and the old password alone.
			assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/me", nil, token).Code)
			assert.Equal(t, http.StatusOK, postJSON(t, s, "/auth/login", loginRequest{Identifier: "tester", Password: "hunter22"}).Code)
		})
	}
}

func TestHandleChangePassword_RequiresSession(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(t, s, http.MethodPut, "/api/v1/me/password", changePasswordRequest{
		CurrentPassword: "hunter22", NewPassword: "correct-horse",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestHandleChangePassword_UnreachableByToken pins that a project API token
// can never take over the account it acts as, the same permanent
// session-only boundary that keeps /api-tokens and member management off
// the bearer allowlist (server.go).
func TestHandleChangePassword_UnreachableByToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPut, "/api/v1/me/password", changePasswordRequest{
		CurrentPassword: "hunter22", NewPassword: "correct-horse",
	}, raw)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
