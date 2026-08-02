package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// createAPITokenRequest's Scopes defaults to read-only when omitted or
// empty, preserving the shape existing clients already send; a caller must
// explicitly ask for ["read","write"] (or just ["write"], which
// apitoken.normalizeScopes expands) to get a token the write-scoped routes
// in server.go's allowlist will accept (issue #66).
type createAPITokenRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// createAPITokenResponse is the API token plus the raw bearer value. Token
// is populated only in this create response — every other response
// (List, and the token embedded in errors) never carries it.
type createAPITokenResponse struct {
	apitoken.APIToken
	Token string `json:"token"`
}

// apiTokenIDFromURL parses the {tokenID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one.
func apiTokenIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "tokenID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleCreateAPIToken issues a new API token for the project, scoped to the
// authenticated user. The raw token is returned only in this response; it
// is never stored or returned again.
func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "api token not found")
		return
	}

	var req createAPITokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{apitoken.ScopeRead}
	}
	token, rawToken, err := s.apiTokens.Create(r.Context(), u.ID, projectID, req.Name, scopes, req.ExpiresAt)
	if err != nil {
		writeAPITokenError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createAPITokenResponse{APIToken: token, Token: rawToken})
}

// handleListAPITokens returns every API token issued for the project, scoped
// to the authenticated user. The raw token value is never included.
func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "api token not found")
		return
	}

	tokens, err := s.apiTokens.List(r.Context(), u.ID, projectID)
	if err != nil {
		writeAPITokenError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// handleDeleteAPIToken revokes one API token, scoped to the authenticated
// user via its project.
func (s *Server) handleDeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	tokenID, ok := apiTokenIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "api token not found")
		return
	}

	if err := s.apiTokens.Delete(r.Context(), u.ID, tokenID); err != nil {
		writeAPITokenError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeAPITokenError maps an apitoken domain error to its HTTP response.
func writeAPITokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apitoken.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-100 characters")
	case errors.Is(err, apitoken.ErrInvalidScopes):
		writeError(w, http.StatusBadRequest, "invalid_scopes", "scopes must be a non-empty subset of read, write")
	case errors.Is(err, apitoken.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "api token not found")
	default:
		slog.Error("api token request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
