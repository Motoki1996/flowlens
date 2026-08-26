package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/epic"
	"github.com/flowlens/api/internal/optional"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// The epic request shapes mirror the backlog ones field for field (see
// backlog_handler.go): an epic is deliberately the same object one rung down,
// so a client that can edit a backlog can edit an epic with the same form.
// backlogId is the only addition.
type createEpicRequest struct {
	// BacklogID files the epic in a backlog; omitted or null leaves it
	// unfiled, the Unclassified group.
	BacklogID   *uuid.UUID `json:"backlogId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	StartDate   *time.Time `json:"startDate"`
	DueOn       *time.Time `json:"dueOn"`
	Priority    string     `json:"priority"`
	Progress    string     `json:"progress"`
	// DefaultLinkedGitlabProjectID overrides where this epic's tasks get their
	// GitLab issue created; omitted or null falls through to the backlog's.
	DefaultLinkedGitlabProjectID *uuid.UUID `json:"defaultLinkedGitlabProjectId"`
	// BaseBranch is the branch tasks in this epic are meant to branch from;
	// omitted leaves it unset and the backlog's stands.
	BaseBranch string `json:"baseBranch"`
	// AllowedScope/ForbiddenScope are the paths tasks in this epic may/may not
	// touch; omitted leaves them unset and the backlog's stand.
	AllowedScope   string `json:"allowedScope"`
	ForbiddenScope string `json:"forbiddenScope"`
	// EstimatedPoints is the epic's pre-breakdown estimate; omitted or null
	// leaves it unestimated. Not a size: it is superseded by the epic's tasks
	// as soon as it has any.
	EstimatedPoints *int `json:"estimatedPoints"`
	// AssigneeUserID is the project member who owns this epic; omitted or null
	// leaves it unassigned.
	AssigneeUserID *uuid.UUID `json:"assigneeUserId"`
}

// Everything except name/description is Optional, so PATCH stays a
// partial update: an absent key keeps the stored value, an explicit null
// clears a nullable field, and an explicit empty string clears baseBranch/
// allowedScope/forbiddenScope (none of which has a null case) or resets
// priority/progress to their default.
type updateEpicRequest struct {
	BacklogID                    optional.Optional[*uuid.UUID] `json:"backlogId"`
	Name                         string                        `json:"name"`
	Description                  string                        `json:"description"`
	StartDate                    optional.Optional[*time.Time] `json:"startDate"`
	DueOn                        optional.Optional[*time.Time] `json:"dueOn"`
	Priority                     optional.Optional[string]     `json:"priority"`
	Progress                     optional.Optional[string]     `json:"progress"`
	DefaultLinkedGitlabProjectID optional.Optional[*uuid.UUID] `json:"defaultLinkedGitlabProjectId"`
	BaseBranch                   optional.Optional[string]     `json:"baseBranch"`
	AllowedScope                 optional.Optional[string]     `json:"allowedScope"`
	ForbiddenScope               optional.Optional[string]     `json:"forbiddenScope"`
	EstimatedPoints              optional.Optional[*int]       `json:"estimatedPoints"`
	AssigneeUserID               optional.Optional[*uuid.UUID] `json:"assigneeUserId"`
}

// setEpicTasksRequest carries the epic's complete task set: every task named
// is filed under it, and every task currently in it that isn't named drops
// out. Declarative rather than add/remove because that is what the screens
// hold — a picker over a backlog's tasks, ticked or not.
type setEpicTasksRequest struct {
	TaskIDs []uuid.UUID `json:"taskIds"`
}

// epicIDFromURL parses the {epicID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one.
func epicIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "epicID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListEpics returns every epic in the project, scoped to the
// authenticated user.
func (s *Server) handleListEpics(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	filter, err := parseEpicListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}

	epics, err := s.epics.List(r.Context(), u.ID, projectID, filter)
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, epics)
}

// parseEpicListFilter reads ?status=, ?backlog_id=, ?priority=, ?progress=,
// ?assignee= and ?sort=, sharing parseAssigneeFilter with the task and backlog
// collections so the assignee vocabulary ("me"/a UUID/"unassigned") means the
// same thing everywhere. ?backlog_id=unassigned narrows to the epics in no
// backlog at all, the same word and spelling the task collection's own
// ?backlog_id= uses.
func parseEpicListFilter(r *http.Request) (epic.ListFilter, error) {
	var filter epic.ListFilter

	// Omitted means open-only, not "no filter" — the same exception the
	// backlog collection makes, and the reason a closed epic leaves the
	// screen without anyone filtering it out.
	if v := r.URL.Query().Get("status"); v != "" {
		if !isValidBacklogStatus(v) {
			return epic.ListFilter{}, errors.New("status must be one of open, closed, all")
		}
		filter.Status = v
	}

	if v := r.URL.Query().Get("backlog_id"); v != "" {
		if v == "unassigned" {
			filter.BacklogUnfiled = true
		} else {
			id, err := uuid.Parse(v)
			if err != nil {
				return epic.ListFilter{}, errors.New("backlog_id must be a UUID or \"unassigned\"")
			}
			filter.BacklogID = &id
		}
	}

	if v := r.URL.Query().Get("priority"); v != "" {
		if !isValidBacklogPriority(v) {
			return epic.ListFilter{}, errors.New("priority must be one of low, medium, high, urgent")
		}
		filter.Priority = v
	}

	if v := r.URL.Query().Get("progress"); v != "" {
		if !isValidBacklogProgress(v) {
			return epic.ListFilter{}, errors.New("progress must be one of not_started, in_progress, on_hold, done")
		}
		filter.Progress = v
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		if v != epic.SortPriority && v != epic.SortProgress {
			return epic.ListFilter{}, errors.New("sort must be one of priority, progress")
		}
		filter.Sort = v
	}

	assigneeID, unassigned, err := parseAssigneeFilter(r)
	if err != nil {
		return epic.ListFilter{}, err
	}
	filter.AssigneeUserID, filter.AssigneeUnassigned = assigneeID, unassigned

	return filter, nil
}

// handleCreateEpic creates an epic in the project,
// scoped to the authenticated user.
func (s *Server) handleCreateEpic(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req createEpicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	e, err := s.epics.Create(r.Context(), u.ID, projectID, epic.CreateParams{
		BacklogID:                    req.BacklogID,
		Name:                         req.Name,
		Description:                  req.Description,
		StartDate:                    req.StartDate,
		DueOn:                        req.DueOn,
		Priority:                     req.Priority,
		Progress:                     req.Progress,
		DefaultLinkedGitlabProjectID: req.DefaultLinkedGitlabProjectID,
		BaseBranch:                   req.BaseBranch,
		AllowedScope:                 req.AllowedScope,
		ForbiddenScope:               req.ForbiddenScope,
		EstimatedPoints:              req.EstimatedPoints,
		AssigneeUserID:               req.AssigneeUserID,
	})
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// handleGetEpic returns one epic, scoped to the authenticated user via its
// project.
func (s *Server) handleGetEpic(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	epicID, ok := epicIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
		return
	}

	e, err := s.epics.Get(r.Context(), u.ID, epicID)
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleUpdateEpic updates one epic, scoped to the authenticated user via its
// project. Unlike a backlog's, an epic's progress change records no event
// row: an epic is a grouping, not something flow metrics measure.
func (s *Server) handleUpdateEpic(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	epicID, ok := epicIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
		return
	}

	var req updateEpicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	e, err := s.epics.Update(r.Context(), u.ID, epicID, epic.UpdateParams{
		BacklogID:                    req.BacklogID,
		Name:                         req.Name,
		Description:                  req.Description,
		StartDate:                    req.StartDate,
		DueOn:                        req.DueOn,
		Priority:                     req.Priority,
		Progress:                     req.Progress,
		DefaultLinkedGitlabProjectID: req.DefaultLinkedGitlabProjectID,
		BaseBranch:                   req.BaseBranch,
		AllowedScope:                 req.AllowedScope,
		ForbiddenScope:               req.ForbiddenScope,
		EstimatedPoints:              req.EstimatedPoints,
		AssigneeUserID:               req.AssigneeUserID,
	})
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleCloseEpic closes one epic, scoped to the authenticated user via its
// project. Closing an already-closed epic is a no-op, and the close never
// cascades to the epic's tasks.
func (s *Server) handleCloseEpic(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	epicID, ok := epicIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
		return
	}

	e, err := s.epics.Close(r.Context(), u.ID, epicID)
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleReopenEpic reopens one epic, scoped to the authenticated user via its
// project. Reopening an already-open epic is a no-op.
func (s *Server) handleReopenEpic(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	epicID, ok := epicIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
		return
	}

	e, err := s.epics.Reopen(r.Context(), u.ID, epicID)
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleDeleteEpic deletes one epic, scoped to the authenticated user via its
// project. Tasks in the epic are not deleted: ON DELETE SET NULL drops them
// back to sitting directly in their backlog.
func (s *Server) handleDeleteEpic(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	epicID, ok := epicIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
		return
	}

	if err := s.epics.Delete(r.Context(), u.ID, epicID); err != nil {
		writeEpicError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetEpicTasks replaces the epic's task set, all-or-nothing. A task
// filed here also moves to the epic's own backlog: a task's epic and backlog
// must always agree.
func (s *Server) handleSetEpicTasks(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	epicID, ok := epicIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
		return
	}

	var req setEpicTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	if err := s.epics.SetTasks(r.Context(), u.ID, epicID, req.TaskIDs); err != nil {
		writeEpicError(w, err)
		return
	}

	e, err := s.epics.Get(r.Context(), u.ID, epicID)
	if err != nil {
		writeEpicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// writeEpicError maps an epic domain error to its HTTP response.
func writeEpicError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, epic.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-100 characters")
	case errors.Is(err, epic.ErrInvalidSchedule):
		writeError(w, http.StatusBadRequest, "invalid_schedule", "start date must not be after due date")
	case errors.Is(err, epic.ErrInvalidPriority):
		writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be one of low, medium, high, urgent")
	case errors.Is(err, epic.ErrInvalidProgress):
		writeError(w, http.StatusBadRequest, "invalid_progress", "progress must be one of not_started, in_progress, on_hold, done")
	case errors.Is(err, epic.ErrInvalidBaseBranch):
		writeError(w, http.StatusBadRequest, "invalid_base_branch", "baseBranch must be a valid git branch name, at most 255 characters")
	case errors.Is(err, epic.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "invalid_scope", "allowedScope/forbiddenScope must be at most 20000 characters")
	case errors.Is(err, epic.ErrInvalidEstimate):
		writeError(w, http.StatusBadRequest, "invalid_estimated_points", "estimatedPoints must be a positive integer")
	case errors.Is(err, epic.ErrAssigneeNotMember):
		writeError(w, http.StatusBadRequest, "invalid_assignee", "assignee must be a member of the project")
	case errors.Is(err, epic.ErrLinkNotInProject):
		writeError(w, http.StatusBadRequest, "invalid_linked_gitlab_project", "defaultLinkedGitlabProjectId must be a GitLab project linked to this project")
	case errors.Is(err, epic.ErrBacklogNotInProject):
		writeError(w, http.StatusBadRequest, "invalid_backlog", "backlogId must be a backlog in this project")
	case errors.Is(err, epic.ErrTaskNotInProject):
		writeError(w, http.StatusBadRequest, "invalid_tasks", "taskIds must all be tasks in this project")
	case errors.Is(err, epic.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "epic not found")
	case errors.Is(err, epic.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("epic request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
