package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/gitlabidentity"
)

// putGitlabIdentityRequest registers or replaces the caller's GitLab
// identity for one GitLab base URL (issue #102).
type putGitlabIdentityRequest struct {
	GitlabBaseURL  string `json:"gitlabBaseUrl"`
	GitlabUserID   int64  `json:"gitlabUserId"`
	GitlabUsername string `json:"gitlabUsername"`
}

// handleListMyGitlabIdentities returns every GitLab identity the
// authenticated user has registered, one per base URL.
func (s *Server) handleListMyGitlabIdentities(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	identities, err := s.gitlabIdentities.ListForUser(r.Context(), u.ID)
	if err != nil {
		slog.Error("gitlab identity list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, identities)
}

// handlePutMyGitlabIdentity registers or replaces the authenticated user's
// GitLab identity for one GitLab base URL, so ?assignee=me on the task
// collections can match tasks assigned to them.
func (s *Server) handlePutMyGitlabIdentity(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req putGitlabIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	identity, err := s.gitlabIdentities.Upsert(r.Context(), u.ID, gitlabidentity.UpsertInput{
		GitlabBaseURL:  req.GitlabBaseURL,
		GitlabUserID:   req.GitlabUserID,
		GitlabUsername: req.GitlabUsername,
	})
	if err != nil {
		if errors.Is(err, gitlabidentity.ErrInvalidBaseURL) {
			writeError(w, http.StatusBadRequest, "invalid_base_url", "gitlabBaseUrl must not be empty")
			return
		}
		slog.Error("gitlab identity upsert", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, identity)
}
