package http

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/project"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// parseProjectID decodes a project ID path parameter back into its raw
// bytes. IDs are round-tripped using the same hex.EncodeToString encoding
// project.FromDB produces, so this must stay in sync with it.
func parseProjectID(raw string) (pgtype.UUID, bool) {
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 16 {
		return pgtype.UUID{}, false
	}
	var id pgtype.UUID
	copy(id.Bytes[:], b)
	id.Valid = true
	return id, true
}

// projectOwnedBy reports whether projectID exists and is owned by userID.
// Shared by every project-scoped resource handler so unauthorized access
// returns 404, never 403, without leaking existence.
func (s *Server) projectOwnedBy(ctx context.Context, projectID, userID pgtype.UUID) (bool, error) {
	_, err := s.projects.Get(ctx, userID, projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// handleListProjects returns every project owned by the authenticated user.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	rows, err := s.projects.List(r.Context(), u.ID)
	if err != nil {
		slog.Error("list projects", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	out := make([]project.Project, len(rows))
	for i, row := range rows {
		out[i] = project.FromDB(row)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateProject creates a project owned by the authenticated user.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	row, err := s.projects.Create(r.Context(), u.ID, req.Name, req.Description)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project.FromDB(row))
}

// handleGetProject returns one project, scoped to the authenticated user.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := parseProjectID(chi.URLParam(r, "projectID"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	row, err := s.projects.Get(r.Context(), u.ID, projectID)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project.FromDB(row))
}

// handleUpdateProject updates one project, scoped to the authenticated user.
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := parseProjectID(chi.URLParam(r, "projectID"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	owned, err := s.projectOwnedBy(r.Context(), projectID, u.ID)
	if err != nil {
		slog.Error("check project ownership", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	row, err := s.projects.Update(r.Context(), projectID, req.Name, req.Description)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project.FromDB(row))
}

// handleDeleteProject deletes one project, scoped to the authenticated user.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := parseProjectID(chi.URLParam(r, "projectID"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	owned, err := s.projectOwnedBy(r.Context(), projectID, u.ID)
	if err != nil {
		slog.Error("check project ownership", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	if err := s.projects.Delete(r.Context(), projectID); err != nil {
		slog.Error("delete project", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
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
