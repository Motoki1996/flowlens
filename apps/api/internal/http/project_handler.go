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
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
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
	default:
		slog.Error("project request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
