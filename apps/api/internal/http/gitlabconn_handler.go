package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/linkedproject"
)

type putGitlabConnectionRequest struct {
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
}

// handlePutGitlabConnection saves (creating or replacing) the GitLab
// connection for one project. The token is verified against the GitLab
// instance before anything is written; a verification failure leaves any
// existing connection untouched and reports 422 with a cause that
// distinguishes unreachable, invalid-token, and insufficient-scope.
func (s *Server) handlePutGitlabConnection(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req putGitlabConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	conn, err := s.gitlabConns.Save(r.Context(), u.ID, projectID, req.BaseURL, req.Token)
	if err != nil {
		writeGitlabConnError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

// handleGetGitlabConnection returns the stored connection's metadata. The
// access token itself is never included, only its last four characters and
// verification status.
func (s *Server) handleGetGitlabConnection(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "gitlab connection not found")
		return
	}

	conn, err := s.gitlabConns.Get(r.Context(), u.ID, projectID)
	if err != nil {
		writeGitlabConnError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

// handleTestGitlabConnection re-verifies a stored connection's token
// against its GitLab instance and persists the outcome.
func (s *Server) handleTestGitlabConnection(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "gitlab connection not found")
		return
	}

	conn, err := s.gitlabConns.Test(r.Context(), u.ID, projectID)
	if err != nil {
		writeGitlabConnError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

// handleDeleteGitlabConnection removes a project's GitLab connection. Every
// GitLab project linked to it is unlinked first (linkedProjects.Delete),
// which deregisters each one's webhook on the GitLab side (issue #18)
// before the connection — and, via cascade, the now-webhook-less link
// rows — is removed.
func (s *Server) handleDeleteGitlabConnection(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "gitlab connection not found")
		return
	}

	links, err := s.linkedProjects.List(r.Context(), u.ID, projectID)
	if err != nil && !errors.Is(err, linkedproject.ErrNotFound) {
		slog.Error("gitlab connection delete: list linked projects", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	for _, link := range links {
		if err := s.linkedProjects.Delete(r.Context(), u.ID, link.ID); err != nil {
			slog.Error("gitlab connection delete: unlink", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
	}

	if err := s.gitlabConns.Delete(r.Context(), u.ID, projectID); err != nil {
		writeGitlabConnError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeGitlabConnError maps a gitlabconn domain error to its HTTP response.
// Verification failures (unreachable / invalid token / insufficient scope)
// are reported as 422, since the request itself was well-formed but the
// GitLab side rejected it.
func writeGitlabConnError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gitlabconn.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "gitlab connection not found")
	case errors.Is(err, gitlabconn.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	case errors.Is(err, gitlabconn.ErrInvalidBaseURL):
		writeError(w, http.StatusBadRequest, "invalid_base_url", "base_url must be an absolute http(s) URL")
	case errors.Is(err, gitlabconn.ErrTokenRequired):
		writeError(w, http.StatusBadRequest, "token_required", "personal access token is required")
	case errors.Is(err, gitlabconn.ErrUnreachable):
		writeError(w, http.StatusUnprocessableEntity, "unreachable", "could not reach the gitlab instance")
	case errors.Is(err, gitlabconn.ErrTokenInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_token", "the personal access token was rejected")
	case errors.Is(err, gitlabconn.ErrInsufficientScope):
		writeError(w, http.StatusUnprocessableEntity, "insufficient_scope", "the personal access token lacks required permissions")
	default:
		slog.Error("gitlab connection request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
