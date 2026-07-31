package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/taskdependency"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createTaskDependencyRequest struct {
	PredecessorTaskID uuid.UUID `json:"predecessorTaskId"`
	SuccessorTaskID   uuid.UUID `json:"successorTaskId"`
}

// taskDependencyIDFromURL parses the {dependencyID} path parameter. A
// malformed ID is reported as "not found" so it is indistinguishable from an
// unknown one.
func taskDependencyIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "dependencyID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListTaskDependencies returns every dependency between tasks in the
// project, scoped to the authenticated user. It backs the project's Gantt
// chart view (issue #33).
func (s *Server) handleListTaskDependencies(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	deps, err := s.taskDependencies.List(r.Context(), u.ID, projectID)
	if err != nil {
		writeTaskDependencyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deps)
}

// handleCreateTaskDependency records that predecessorTaskId must finish
// before successorTaskId starts, scoped to the authenticated user. Both
// tasks must belong to the project, and the new edge must not close a cycle.
func (s *Server) handleCreateTaskDependency(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req createTaskDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	d, err := s.taskDependencies.Create(r.Context(), u.ID, projectID, req.PredecessorTaskID, req.SuccessorTaskID)
	if err != nil {
		writeTaskDependencyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// handleDeleteTaskDependency removes one dependency, scoped to the
// authenticated user via its predecessor task's project.
func (s *Server) handleDeleteTaskDependency(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	dependencyID, ok := taskDependencyIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "dependency not found")
		return
	}

	if err := s.taskDependencies.Delete(r.Context(), u.ID, dependencyID); err != nil {
		writeTaskDependencyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeTaskDependencyError maps a taskdependency domain error to its HTTP
// response.
func writeTaskDependencyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, taskdependency.ErrSelfDependency):
		writeError(w, http.StatusBadRequest, "self_dependency", "a task cannot depend on itself")
	case errors.Is(err, taskdependency.ErrTaskNotInProject):
		writeError(w, http.StatusBadRequest, "invalid_task", "task belongs to a different project")
	case errors.Is(err, taskdependency.ErrCyclicDependency):
		writeError(w, http.StatusConflict, "cyclic_dependency", "would create a cyclic dependency")
	case errors.Is(err, taskdependency.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "dependency not found")
	default:
		slog.Error("task dependency request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
