package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/task"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createTaskRequest struct {
	Title                  string     `json:"title"`
	Description            string     `json:"description"`
	BacklogID              *uuid.UUID `json:"backlogId"`
	AssigneeGitlabUserID   *int64     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string     `json:"assigneeGitlabUsername"`
	Labels                 []string   `json:"labels"`
	DueOn                  *time.Time `json:"dueOn"`
	StartDate              *time.Time `json:"startDate"`
}

type updateTaskRequest struct {
	Title                  string     `json:"title"`
	Description            string     `json:"description"`
	BacklogID              *uuid.UUID `json:"backlogId"`
	AssigneeGitlabUserID   *int64     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string     `json:"assigneeGitlabUsername"`
	Labels                 []string   `json:"labels"`
	DueOn                  *time.Time `json:"dueOn"`
	StartDate              *time.Time `json:"startDate"`
	Position               int32      `json:"position"`
}

type assignTaskBacklogRequest struct {
	BacklogID *uuid.UUID `json:"backlogId"`
}

type upsertTaskAIContextRequest struct {
	AcceptanceCriteria string `json:"acceptanceCriteria"`
	AIContext          string `json:"aiContext"`
	AllowedScope       string `json:"allowedScope"`
	ForbiddenScope     string `json:"forbiddenScope"`
}

// taskIDFromURL parses the {taskID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one.
func taskIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// parseTaskListFilter reads the ?backlog_id= and ?status= query parameters.
// backlog_id accepts a UUID or the literal "unassigned"; status accepts
// "open" or "closed". Either may be omitted to mean "no filter".
func parseTaskListFilter(r *http.Request) (task.ListFilter, error) {
	var filter task.ListFilter

	if v := r.URL.Query().Get("backlog_id"); v != "" {
		if v == "unassigned" {
			filter.Unassigned = true
		} else {
			id, err := uuid.Parse(v)
			if err != nil {
				return task.ListFilter{}, errors.New("backlog_id must be a UUID or \"unassigned\"")
			}
			filter.BacklogID = &id
		}
	}

	if v := r.URL.Query().Get("status"); v != "" {
		if v != task.StatusOpen && v != task.StatusClosed {
			return task.ListFilter{}, errors.New("status must be \"open\" or \"closed\"")
		}
		filter.Status = v
	}

	return filter, nil
}

// handleListTasks returns the project's tasks matching the backlog_id/status
// query filters, scoped to the authenticated user.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	filter, err := parseTaskListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}

	tasks, err := s.tasks.List(r.Context(), u.ID, projectID, filter)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// handleCreateTask creates a task in the project, scoped to the
// authenticated user. A nil backlogId leaves the task unfiled (未分類).
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	t, err := s.tasks.Create(r.Context(), u.ID, projectID, task.CreateParams{
		Title:                  req.Title,
		Description:            req.Description,
		BacklogID:              req.BacklogID,
		AssigneeGitlabUserID:   req.AssigneeGitlabUserID,
		AssigneeGitlabUsername: req.AssigneeGitlabUsername,
		Labels:                 req.Labels,
		DueOn:                  req.DueOn,
		StartDate:              req.StartDate,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// handleGetTask returns one task, scoped to the authenticated user via its
// project.
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	t, err := s.tasks.Get(r.Context(), u.ID, taskID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	aiContext, err := s.tasks.GetAIContext(r.Context(), u.ID, taskID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	t.AIContext = aiContext
	writeJSON(w, http.StatusOK, t)
}

// handleUpdateTask updates one task, scoped to the authenticated user via
// its project. A nil backlogId leaves the task unfiled (未分類); status only
// changes through /close and /reopen.
func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	t, err := s.tasks.Update(r.Context(), u.ID, taskID, task.UpdateParams{
		Title:                  req.Title,
		Description:            req.Description,
		BacklogID:              req.BacklogID,
		AssigneeGitlabUserID:   req.AssigneeGitlabUserID,
		AssigneeGitlabUsername: req.AssigneeGitlabUsername,
		Labels:                 req.Labels,
		DueOn:                  req.DueOn,
		StartDate:              req.StartDate,
		Position:               req.Position,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleDeleteTask deletes one task, scoped to the authenticated user via
// its project. This never touches GitLab: no GitLab connection exists yet,
// and it must not once sync ships either (docs/plans/issue-sync.md).
func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	if err := s.tasks.Delete(r.Context(), u.ID, taskID); err != nil {
		writeTaskError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAssignTaskBacklog moves one task to a backlog (or back to unfiled,
// 未分類, when backlogId is null), scoped to the authenticated user via its
// project. Unlike handleUpdateTask, it only touches backlog_id, so callers
// don't need to resend the rest of the task to move it.
func (s *Server) handleAssignTaskBacklog(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	var req assignTaskBacklogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	t, err := s.tasks.AssignBacklog(r.Context(), u.ID, taskID, req.BacklogID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleCloseTask closes one task, scoped to the authenticated user via its
// project. Closing an already-closed task is a no-op.
func (s *Server) handleCloseTask(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	t, err := s.tasks.Close(r.Context(), u.ID, taskID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleReopenTask reopens one task, scoped to the authenticated user via
// its project. Reopening an already-open task is a no-op.
func (s *Server) handleReopenTask(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	t, err := s.tasks.Reopen(r.Context(), u.ID, taskID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleRetryTaskSync re-enqueues one task's most recent failed GitLab push,
// scoped to the authenticated user via its project. It returns 409 unless
// the task's current sync status is "failed".
func (s *Server) handleRetryTaskSync(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	t, err := s.tasks.RetrySync(r.Context(), u.ID, taskID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleUpsertTaskAIContext creates or overwrites one task's AI context,
// scoped to the authenticated user via its project. These fields are
// app-only and are never sent to GitLab (docs/plans/issue-sync.md).
func (s *Server) handleUpsertTaskAIContext(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	var req upsertTaskAIContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	aiContext, err := s.tasks.UpsertAIContext(r.Context(), u.ID, taskID, task.AIContextParams{
		AcceptanceCriteria: req.AcceptanceCriteria,
		AIContext:          req.AIContext,
		AllowedScope:       req.AllowedScope,
		ForbiddenScope:     req.ForbiddenScope,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, aiContext)
}

// writeTaskError maps a task domain error to its HTTP response.
func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, task.ErrInvalidTitle):
		writeError(w, http.StatusBadRequest, "invalid_title", "title must be 1-255 characters")
	case errors.Is(err, task.ErrBacklogNotInProject):
		writeError(w, http.StatusBadRequest, "invalid_backlog", "backlog belongs to a different project")
	case errors.Is(err, task.ErrAIContextFieldTooLong):
		writeError(w, http.StatusBadRequest, "ai_context_field_too_long", "AI context fields must be at most 20000 characters")
	case errors.Is(err, task.ErrSyncNotFailed):
		writeError(w, http.StatusConflict, "sync_not_failed", "gitlab sync is not currently failed")
	case errors.Is(err, task.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	default:
		slog.Error("task request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
