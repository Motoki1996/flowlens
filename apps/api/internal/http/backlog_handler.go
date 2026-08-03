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
	Priority    string     `json:"priority"`
}

// The dates and priority are Optional so PATCH stays a partial update for
// them: a body without "startDate"/"priority" keeps the stored value, and an
// explicit null clears a date (priority has no null case — see
// backlog.normalizePriority). Name/description/position predate that and are
// still overwritten wholesale.
type updateBacklogRequest struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description"`
	Position    int32                         `json:"position"`
	StartDate   optional.Optional[*time.Time] `json:"startDate"`
	DueOn       optional.Optional[*time.Time] `json:"dueOn"`
	Priority    optional.Optional[string]     `json:"priority"`
}

// reorderBacklogsRequest carries a project's full, newly-ordered backlog ID
// list (issue #79). backlogIds must be exactly the project's current
// backlogs — see backlog.Service.Reorder.
type reorderBacklogsRequest struct {
	BacklogIDs []uuid.UUID `json:"backlogIds"`
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

	filter, err := parseBacklogListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}

	backlogs, err := s.backlogs.List(r.Context(), u.ID, projectID, filter)
	if err != nil {
		writeBacklogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backlogs)
}

// handleReorderBacklogs resequences a project's backlogs to the given order
// in a single request, scoped to the authenticated user (issue #79).
// backlogIds must be exactly the project's current backlogs; a mismatched
// set is rejected as a whole rather than partially applied.
func (s *Server) handleReorderBacklogs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req reorderBacklogsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	backlogs, err := s.backlogs.Reorder(r.Context(), u.ID, projectID, req.BacklogIDs)
	if err != nil {
		writeBacklogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backlogs)
}

// isValidBacklogPriority reports whether v is one of the fixed priority
// values.
func isValidBacklogPriority(v string) bool {
	switch v {
	case backlog.PriorityLow, backlog.PriorityMedium, backlog.PriorityHigh, backlog.PriorityUrgent:
		return true
	default:
		return false
	}
}

// parseBacklogListFilter reads the ?priority= and ?sort= query parameters,
// the same way parseTaskListFilter does for tasks. Either may be omitted to
// mean "no filter"/"default order".
func parseBacklogListFilter(r *http.Request) (backlog.ListFilter, error) {
	var filter backlog.ListFilter

	if v := r.URL.Query().Get("priority"); v != "" {
		if !isValidBacklogPriority(v) {
			return backlog.ListFilter{}, errors.New("priority must be one of low, medium, high, urgent")
		}
		filter.Priority = v
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		if v != backlog.SortPriority {
			return backlog.ListFilter{}, errors.New("sort must be \"priority\"")
		}
		filter.Sort = v
	}

	return filter, nil
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
		Priority:    req.Priority,
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
		Priority:    req.Priority,
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
	case errors.Is(err, backlog.ErrInvalidPriority):
		writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be one of low, medium, high, urgent")
	case errors.Is(err, backlog.ErrBacklogIDsMismatch):
		writeError(w, http.StatusBadRequest, "backlog_ids_mismatch", "backlogIds must exactly match the project's current backlogs")
	case errors.Is(err, backlog.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "backlog not found")
	default:
		slog.Error("backlog request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
