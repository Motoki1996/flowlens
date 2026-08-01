package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/optional"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createBacklogRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	StartDate   *time.Time `json:"startDate"`
	DueOn       *time.Time `json:"dueOn"`
}

// The dates are Optional so PATCH stays a partial update for them: a body
// without "startDate" keeps the stored one, and an explicit null clears it.
// Name/description/position predate that and are still overwritten wholesale.
type updateBacklogRequest struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description"`
	Position    int32                         `json:"position"`
	StartDate   optional.Optional[*time.Time] `json:"startDate"`
	DueOn       optional.Optional[*time.Time] `json:"dueOn"`
}

// backlogIDFromURL parses the {backlogID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one.
func backlogIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "backlogID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListBacklogs returns every backlog in the project, scoped to the
// authenticated user.
func (s *Server) handleListBacklogs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	backlogs, err := s.backlogs.List(r.Context(), u.ID, projectID)
	if err != nil {
		writeBacklogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backlogs)
}

// handleCreateBacklog creates a backlog at the end of the project's backlog
// order, scoped to the authenticated user.
func (s *Server) handleCreateBacklog(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req createBacklogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	b, err := s.backlogs.Create(r.Context(), u.ID, projectID, backlog.CreateParams{
		Name:        req.Name,
		Description: req.Description,
		StartDate:   req.StartDate,
		DueOn:       req.DueOn,
	})
	if err != nil {
		writeBacklogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

// handleGetBacklog returns one backlog, scoped to the authenticated user via
// its project.
func (s *Server) handleGetBacklog(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	backlogID, ok := backlogIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "backlog not found")
		return
	}

	b, err := s.backlogs.Get(r.Context(), u.ID, backlogID)
	if err != nil {
		writeBacklogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleUpdateBacklog updates one backlog, scoped to the authenticated user
// via its project.
func (s *Server) handleUpdateBacklog(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	backlogID, ok := backlogIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "backlog not found")
		return
	}

	var req updateBacklogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	b, err := s.backlogs.Update(r.Context(), u.ID, backlogID, backlog.UpdateParams{
		Name:        req.Name,
		Description: req.Description,
		Position:    req.Position,
		StartDate:   req.StartDate,
		DueOn:       req.DueOn,
	})
	if err != nil {
		writeBacklogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleDeleteBacklog deletes one backlog, scoped to the authenticated user
// via its project. Tasks in the backlog are not deleted: ON DELETE SET NULL
// drops them to unfiled (backlog_id = NULL).
func (s *Server) handleDeleteBacklog(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	backlogID, ok := backlogIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "backlog not found")
		return
	}

	if err := s.backlogs.Delete(r.Context(), u.ID, backlogID); err != nil {
		writeBacklogError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeBacklogError maps a backlog domain error to its HTTP response.
func writeBacklogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backlog.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-100 characters")
	case errors.Is(err, backlog.ErrInvalidSchedule):
		writeError(w, http.StatusBadRequest, "invalid_schedule", "start date must not be after due date")
	case errors.Is(err, backlog.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "backlog not found")
	default:
		slog.Error("backlog request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
