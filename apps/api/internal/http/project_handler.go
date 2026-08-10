package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/project"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// projectIDFromURL parses the {projectID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one.
func projectIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListProjects returns every project owned by the authenticated user.
//
// A bearer-authenticated request is scoped down to just its own token's
// project (issue #66): unlike every other handler, this one cannot lean on
// userFromContext's owner-scoped List alone, since the token's owner may
// have other projects the token was never issued for.
//
// ?failedSync=true narrows to projects with at least one failed-sync task,
// for the dashboard's "sync failures" section (issue #77). It is ignored on
// a bearer-authenticated request, which always returns its single project
// regardless — a project token has no notion of "every project I own" to
// narrow in the first place.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	if ts, ok := tokenScopeFromContext(r.Context()); ok {
		p, err := s.projects.Get(r.Context(), u.ID, ts.ProjectID)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, []project.Project{p})
		return
	}

	if r.URL.Query().Get("failedSync") == "true" {
		projects, err := s.projects.ListFailedSync(r.Context(), u.ID)
		if err != nil {
			slog.Error("list failed sync projects", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, projects)
		return
	}

	projects, err := s.projects.List(r.Context(), u.ID)
	if err != nil {
		slog.Error("list projects", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

// handleCreateProject creates a project owned by the authenticated user.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	p, err := s.projects.Create(r.Context(), u.ID, req.Name, req.Description)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// handleGetProject returns one project, scoped to the authenticated user.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	p, err := s.projects.Get(r.Context(), u.ID, projectID)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	failedCount, err := s.projects.FailedSyncTaskCount(r.Context(), u.ID, projectID)
	if err != nil {
		slog.Error("get project", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	p.FailedSyncTaskCount = failedCount
	writeJSON(w, http.StatusOK, p)
}

// handleUpdateProject updates one project, scoped to the authenticated user.
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	p, err := s.projects.Update(r.Context(), u.ID, projectID, req.Name, req.Description)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeleteProject deletes one project, scoped to the authenticated user.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	if err := s.projects.Delete(r.Context(), u.ID, projectID); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProjectError maps a project domain error to its HTTP response.
func writeProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, project.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-100 characters")
	case errors.Is(err, project.ErrNameTaken):
		writeError(w, http.StatusConflict, "name_taken", "a project with this name already exists")
	case errors.Is(err, project.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case errors.Is(err, project.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("project request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
