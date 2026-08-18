package http

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Priority               string     `json:"priority"`
	Progress               string     `json:"progress"`
}

// updateTaskRequest is a true partial update: a key absent from the body
// leaves that field alone, while an explicit null on a nullable field clears
// it. See task.Optional for why a plain pointer is not enough.
type updateTaskRequest struct {
	Title                  task.Optional[string]     `json:"title"`
	Description            task.Optional[string]     `json:"description"`
	BacklogID              task.Optional[*uuid.UUID] `json:"backlogId"`
	AssigneeGitlabUserID   task.Optional[*int64]     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername task.Optional[string]     `json:"assigneeGitlabUsername"`
	Labels                 task.Optional[[]string]   `json:"labels"`
	DueOn                  task.Optional[*time.Time] `json:"dueOn"`
	StartDate              task.Optional[*time.Time] `json:"startDate"`
	Priority               task.Optional[string]     `json:"priority"`
	Progress               task.Optional[string]     `json:"progress"`
	Position               task.Optional[int32]      `json:"position"`
}

type assignTaskBacklogRequest struct {
	BacklogID *uuid.UUID `json:"backlogId"`
}

// reorderTasksRequest carries one backlog bucket's full, newly-ordered task
// ID list (issue #79): BacklogID nil targets the Unclassified group, a UUID
// targets that specific backlog. TaskIDs must be exactly that bucket's
// current tasks — see task.Service.Reorder.
type reorderTasksRequest struct {
	BacklogID *uuid.UUID  `json:"backlogId"`
	TaskIDs   []uuid.UUID `json:"taskIds"`
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

// isValidPriority reports whether v is one of the fixed priority values.
func isValidPriority(v string) bool {
	switch v {
	case task.PriorityLow, task.PriorityMedium, task.PriorityHigh, task.PriorityUrgent:
		return true
	default:
		return false
	}
}

// isValidProgress reports whether v is one of the fixed progress values.
func isValidProgress(v string) bool {
	switch v {
	case task.ProgressNotStarted, task.ProgressInProgress, task.ProgressOnHold, task.ProgressDone:
		return true
	default:
		return false
	}
}

// parseTaskListFilter reads the ?backlog_id=, ?status=, ?priority=,
// ?progress=, ?sort=, ?assignee= and ?q= query parameters. backlog_id
// accepts a UUID or the literal "unassigned"; status accepts "open" or
// "closed" (the GitLab issue state); priority accepts "low", "medium",
// "high" or "urgent"; progress accepts "not_started", "in_progress",
// "on_hold" or "done" (FlowLens's own work state, independent of status);
// sort accepts "priority" or "progress" to rank by either instead of the
// default manual/position order; assignee accepts only the literal "me"
// (issue #102); q free-text matches a task's title or description (issue
// #106). Any may be omitted to mean "no filter"/"default order".
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

	if v := r.URL.Query().Get("priority"); v != "" {
		if !isValidPriority(v) {
			return task.ListFilter{}, errors.New("priority must be one of low, medium, high, urgent")
		}
		filter.Priority = v
	}

	if v := r.URL.Query().Get("progress"); v != "" {
		if !isValidProgress(v) {
			return task.ListFilter{}, errors.New("progress must be one of not_started, in_progress, on_hold, done")
		}
		filter.Progress = v
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		// The same four values the cross-project collection accepts (see
		// parseCrossProjectFilter), so a screen's sort menu means the same
		// thing whichever list backs it. Omitting sort is this list's manual
		// position order, which the cross-project one has no equivalent for.
		if v != task.SortPriority && v != task.SortProgress && v != task.SortDueOn && v != task.SortUpdatedAt {
			return task.ListFilter{}, errors.New("sort must be one of priority, progress, dueOn, updatedAt")
		}
		filter.Sort = v
	}

	if v := r.URL.Query().Get("assignee"); v != "" {
		if v != "me" {
			return task.ListFilter{}, errors.New("assignee must be \"me\"")
		}
		filter.AssigneeMe = true
	}

	filter.Query = r.URL.Query().Get("q")

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

// handleReorderTasks resequences one backlog bucket's tasks to the given
// order in a single request, scoped to the authenticated user (issue #79).
// A backlogId of null targets the Unclassified group. taskIds must be
// exactly that bucket's current tasks; a mismatched set is rejected as a
// whole rather than partially applied.
func (s *Server) handleReorderTasks(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req reorderTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	tasks, err := s.tasks.Reorder(r.Context(), u.ID, projectID, req.BacklogID, req.TaskIDs)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// parseDateQueryParam reads name as a YYYY-MM-DD date, mirroring the format
// internal/webhookapply and internal/projectsync already parse task/backlog
// dates with. An absent value is not an error — it means "no filter" — but a
// present, malformed one is.
func parseDateQueryParam(r *http.Request, name string) (*time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, fmt.Errorf("%s must be a YYYY-MM-DD date", name)
	}
	return &t, nil
}

// isValidCrossProjectSort reports whether v is one of the sort values GET
// /api/v1/tasks accepts.
func isValidCrossProjectSort(v string) bool {
	switch v {
	case task.SortDueOn, task.SortPriority, task.SortProgress, task.SortUpdatedAt:
		return true
	default:
		return false
	}
}

// parseCrossProjectTaskListFilter reads the query parameters GET
// /api/v1/tasks accepts (issue #76): ?status=, ?priority=, ?progress=, ?dueBefore=,
// ?dueAfter=, ?startedBefore= (all YYYY-MM-DD), ?projectId= (repeatable —
// still scoped to the caller's own projects, never a way to reach someone
// else's), ?sort=, ?limit=, ?assignee= (only the literal "me", issue #102)
// and ?q= (free-text, matching a task's title or description, issue #106).
// Every filter may be omitted; sort and limit
// are defaulted by task.Service.ListForOwner itself, not here.
func parseCrossProjectTaskListFilter(r *http.Request) (task.CrossProjectFilter, error) {
	var filter task.CrossProjectFilter

	if v := r.URL.Query().Get("status"); v != "" {
		if v != task.StatusOpen && v != task.StatusClosed {
			return task.CrossProjectFilter{}, errors.New("status must be \"open\" or \"closed\"")
		}
		filter.Status = v
	}

	if v := r.URL.Query().Get("priority"); v != "" {
		if !isValidPriority(v) {
			return task.CrossProjectFilter{}, errors.New("priority must be one of low, medium, high, urgent")
		}
		filter.Priority = v
	}

	if v := r.URL.Query().Get("progress"); v != "" {
		if !isValidProgress(v) {
			return task.CrossProjectFilter{}, errors.New("progress must be one of not_started, in_progress, on_hold, done")
		}
		filter.Progress = v
	}

	dueBefore, err := parseDateQueryParam(r, "dueBefore")
	if err != nil {
		return task.CrossProjectFilter{}, err
	}
	filter.DueBefore = dueBefore

	dueAfter, err := parseDateQueryParam(r, "dueAfter")
	if err != nil {
		return task.CrossProjectFilter{}, err
	}
	filter.DueAfter = dueAfter

	startedBefore, err := parseDateQueryParam(r, "startedBefore")
	if err != nil {
		return task.CrossProjectFilter{}, err
	}
	filter.StartedBefore = startedBefore

	for _, v := range r.URL.Query()["projectId"] {
		id, err := uuid.Parse(v)
		if err != nil {
			return task.CrossProjectFilter{}, errors.New("projectId must be a UUID")
		}
		filter.ProjectIDs = append(filter.ProjectIDs, id)
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		if !isValidCrossProjectSort(v) {
			return task.CrossProjectFilter{}, errors.New("sort must be one of dueOn, priority, progress, updatedAt")
		}
		filter.Sort = v
	}

	filter.Limit = atoiOrZero(r.URL.Query().Get("limit"))

	if v := r.URL.Query().Get("assignee"); v != "" {
		if v != "me" {
			return task.CrossProjectFilter{}, errors.New("assignee must be \"me\"")
		}
		filter.AssigneeMe = true
	}

	filter.Query = r.URL.Query().Get("q")

	return filter, nil
}

// handleListAllTasks returns every task across every project the
// authenticated user owns (GET /api/v1/tasks, issue #76) — the task
// collection's cross-project entry point, "what should I be doing right
// now" without opening each project separately. Session-only: see this
// route's registration in server.go for why a project-scoped API token
// (ADR-0009) never reaches it.
func (s *Server) handleListAllTasks(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	filter, err := parseCrossProjectTaskListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}

	tasks, err := s.tasks.ListForOwner(r.Context(), u.ID, filter)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// handleCreateTask creates a task in the project, scoped to the
// authenticated user. A nil backlogId leaves the task unfiled (Unclassified).
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
		Priority:               req.Priority,
		Progress:               req.Progress,
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
// its project. A nil backlogId leaves the task unfiled (Unclassified); status only
// changes through /close and /reopen. A progress change is attributed to a
// task_progress_events row (issue #169) as ActorKindAgent for a bearer
// caller (this route is shared, per requireAuthOrBearer) or ActorKindUser
// for a session caller.
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

	actorKind := task.ActorKindUser
	if _, ok := tokenScopeFromContext(r.Context()); ok {
		actorKind = task.ActorKindAgent
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
		Priority:               req.Priority,
		Progress:               req.Progress,
		Position:               req.Position,
	}, actorKind)
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
// Unclassified, when backlogId is null), scoped to the authenticated user via its
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

// handleMarkTaskDesignStarted records the current time as the task's design
// phase start (spec-driven development), scoped to the authenticated user
// via its project. Always overwrites any earlier value.
func (s *Server) handleMarkTaskDesignStarted(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	t, err := s.tasks.MarkDesignStarted(r.Context(), u.ID, taskID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleMarkTaskImplementationStarted is handleMarkTaskDesignStarted's
// implementation-phase counterpart.
func (s *Server) handleMarkTaskImplementationStarted(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	t, err := s.tasks.MarkImplementationStarted(r.Context(), u.ID, taskID)
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
	case errors.Is(err, task.ErrInvalidPriority):
		writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be one of low, medium, high, urgent")
	case errors.Is(err, task.ErrInvalidProgress):
		writeError(w, http.StatusBadRequest, "invalid_progress", "progress must be one of not_started, in_progress, on_hold, done")
	case errors.Is(err, task.ErrBacklogNotInProject):
		writeError(w, http.StatusBadRequest, "invalid_backlog", "backlog belongs to a different project")
	case errors.Is(err, task.ErrAIContextFieldTooLong):
		writeError(w, http.StatusBadRequest, "ai_context_field_too_long", "AI context fields must be at most 20000 characters")
	case errors.Is(err, task.ErrSyncNotFailed):
		writeError(w, http.StatusConflict, "sync_not_failed", "gitlab sync is not currently failed")
	case errors.Is(err, task.ErrTaskIDsMismatch):
		writeError(w, http.StatusBadRequest, "task_ids_mismatch", "taskIds must exactly match the current tasks in that backlog")
	case errors.Is(err, task.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case errors.Is(err, task.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("task request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
