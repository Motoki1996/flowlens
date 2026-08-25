package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/metricsperiod"
	"github.com/flowlens/api/internal/task"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createTaskRequest struct {
	Title                  string     `json:"title"`
	Description            string     `json:"description"`
	BacklogID              *uuid.UUID `json:"backlogId"`
	EpicID                 *uuid.UUID `json:"epicId"`
	AssigneeGitlabUserID   *int64     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string     `json:"assigneeGitlabUsername"`
	AssigneeUserID         *uuid.UUID `json:"assigneeUserId"`
	Labels                 []string   `json:"labels"`
	DueOn                  *time.Time `json:"dueOn"`
	StartDate              *time.Time `json:"startDate"`
	Priority               string     `json:"priority"`
	Progress               string     `json:"progress"`
	Size                   string     `json:"size"`
}

// updateTaskRequest is a true partial update: a key absent from the body
// leaves that field alone, while an explicit null on a nullable field clears
// it. See task.Optional for why a plain pointer is not enough.
type updateTaskRequest struct {
	Title                  task.Optional[string]     `json:"title"`
	Description            task.Optional[string]     `json:"description"`
	BacklogID              task.Optional[*uuid.UUID] `json:"backlogId"`
	EpicID                 task.Optional[*uuid.UUID] `json:"epicId"`
	AssigneeGitlabUserID   task.Optional[*int64]     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername task.Optional[string]     `json:"assigneeGitlabUsername"`
	AssigneeUserID         task.Optional[*uuid.UUID] `json:"assigneeUserId"`
	Labels                 task.Optional[[]string]   `json:"labels"`
	DueOn                  task.Optional[*time.Time] `json:"dueOn"`
	StartDate              task.Optional[*time.Time] `json:"startDate"`
	Priority               task.Optional[string]     `json:"priority"`
	Progress               task.Optional[string]     `json:"progress"`
	Size                   task.Optional[string]     `json:"size"`
}

type assignTaskBacklogRequest struct {
	BacklogID *uuid.UUID `json:"backlogId"`
}

// bulkCreateTasksRequest is POST .../tasks/bulk's body: a batch of tasks
// (each keyed by a request-scoped ref) plus dependencies between them,
// created together in one transaction. See task.BulkCreateParams.
type bulkCreateTasksRequest struct {
	Tasks        []bulkTaskRequest       `json:"tasks"`
	Dependencies []bulkDependencyRequest `json:"dependencies"`
}

type bulkTaskRequest struct {
	Ref            string                      `json:"ref"`
	Title          string                      `json:"title"`
	Description    string                      `json:"description"`
	BacklogID      *uuid.UUID                  `json:"backlogId"`
	EpicID         *uuid.UUID                  `json:"epicId"`
	AssigneeUserID *uuid.UUID                  `json:"assigneeUserId"`
	Labels         []string                    `json:"labels"`
	DueOn          *time.Time                  `json:"dueOn"`
	StartDate      *time.Time                  `json:"startDate"`
	Priority       string                      `json:"priority"`
	Size           string                      `json:"size"`
	AIContext      *upsertTaskAIContextRequest `json:"aiContext"`
}

type bulkDependencyRequest struct {
	PredecessorRef string `json:"predecessorRef"`
	SuccessorRef   string `json:"successorRef"`
}

type upsertTaskAIContextRequest struct {
	AcceptanceCriteria string `json:"acceptanceCriteria"`
	AIContext          string `json:"aiContext"`
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

// isValidSize reports whether v is one of the fixed size values.
func isValidSize(v string) bool {
	switch v {
	case task.SizeXS, task.SizeS, task.SizeM, task.SizeL, task.SizeXL:
		return true
	default:
		return false
	}
}

// parseTaskListFilter reads the ?backlog_id=, ?epic_id=, ?status=,
// ?priority=, ?progress=, ?sort=, ?assignee= and ?q= query parameters.
// backlog_id and epic_id each accept a UUID or the literal "unassigned"
// (for epic_id that means the tasks sitting directly in a backlog, which is
// every task predating the epic layer); status accepts "open" or
// "closed" (the GitLab issue state); priority accepts "low", "medium",
// "high" or "urgent"; progress accepts "not_started", "in_progress",
// "on_hold" or "done" (FlowLens's own work state, independent of status);
// sort accepts "priority" or "progress" to rank by either instead of the
// default creation order; assignee accepts "me", a user UUID or
// "unassigned" (issue #102, widened to any member by 000031); q free-text
// matches a task's title or description (issue #106). Any may be omitted to
// mean "no filter"/"default order".
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

	if v := r.URL.Query().Get("epic_id"); v != "" {
		if v == "unassigned" {
			filter.EpicUnfiled = true
		} else {
			id, err := uuid.Parse(v)
			if err != nil {
				return task.ListFilter{}, errors.New("epic_id must be a UUID or \"unassigned\"")
			}
			filter.EpicID = &id
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

	if v := r.URL.Query().Get("size"); v != "" {
		if !isValidSize(v) {
			return task.ListFilter{}, errors.New("size must be one of xs, s, m, l, xl")
		}
		filter.Size = v
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		// The same five values the cross-project collection accepts (see
		// parseCrossProjectFilter), so a screen's sort menu means the same
		// thing whichever list backs it. Omitting sort leaves both lists in
		// their objects' creation order.
		if v != task.SortPriority && v != task.SortProgress && v != task.SortSize && v != task.SortDueOn && v != task.SortUpdatedAt {
			return task.ListFilter{}, errors.New("sort must be one of priority, progress, size, dueOn, updatedAt")
		}
		filter.Sort = v
	}

	assigneeID, unassigned, err := parseAssigneeFilter(r)
	if err != nil {
		return task.ListFilter{}, err
	}
	filter.AssigneeUserID, filter.AssigneeUnassigned = assigneeID, unassigned

	filter.Query = r.URL.Query().Get("q")

	return filter, nil
}

// parseAssigneeFilter reads ?assignee=, shared by the project-scoped and
// cross-project task collections and by the backlog collection so the three
// can never disagree about what a value means. It accepts "me" (resolved to
// the caller's own user ID, so the rest of the stack has one code path), a
// user UUID, or "unassigned". An unparseable value is an error rather than a
// silently ignored filter — a typo'd assignee that returns everyone else's
// work is worse than a 400.
func parseAssigneeFilter(r *http.Request) (*uuid.UUID, bool, error) {
	v := r.URL.Query().Get("assignee")
	if v == "" {
		return nil, false, nil
	}
	if v == "unassigned" {
		return nil, true, nil
	}
	if v == "me" {
		// Every route this parses for is behind auth, and a bearer token
		// resolves to its project's owner (internal/apitoken, ADR-0009), so
		// "me" always has someone to mean. The guard is defensive only.
		u, ok := userFromContext(r.Context())
		if !ok {
			return nil, false, errors.New("assignee=me requires an authenticated caller")
		}
		id := u.ID
		return &id, false, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, false, errors.New("assignee must be \"me\", \"unassigned\" or a user UUID")
	}
	return &id, false, nil
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

// parseIntervalQueryParam reads ?interval= for the metrics endpoints (issue
// #188): "week", "month", "year", or absent, meaning "don't bucket". An
// unrecognized value is an error, same treatment as a malformed from/to.
func parseIntervalQueryParam(r *http.Request) (*metricsperiod.Interval, error) {
	v := r.URL.Query().Get("interval")
	if v == "" {
		return nil, nil
	}
	interval, ok := metricsperiod.ParseInterval(v)
	if !ok {
		return nil, fmt.Errorf("interval must be week, month, or year")
	}
	return &interval, nil
}

// isValidCrossProjectSort reports whether v is one of the sort values GET
// /api/v1/tasks accepts.
func isValidCrossProjectSort(v string) bool {
	switch v {
	case task.SortDueOn, task.SortPriority, task.SortProgress, task.SortSize, task.SortUpdatedAt:
		return true
	default:
		return false
	}
}

// parseCrossProjectTaskListFilter reads the query parameters GET
// /api/v1/tasks accepts (issue #76): ?status=, ?priority=, ?progress=, ?dueBefore=,
// ?dueAfter=, ?startedBefore= (all YYYY-MM-DD), ?projectId= (repeatable —
// still scoped to the caller's own projects, never a way to reach someone
// else's), ?sort=, ?limit=, ?assignee= ("me", a user UUID or "unassigned")
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

	if v := r.URL.Query().Get("size"); v != "" {
		if !isValidSize(v) {
			return task.CrossProjectFilter{}, errors.New("size must be one of xs, s, m, l, xl")
		}
		filter.Size = v
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

	if v := r.URL.Query().Get("epic_id"); v != "" {
		if v == "unassigned" {
			filter.EpicUnfiled = true
		} else {
			id, err := uuid.Parse(v)
			if err != nil {
				return task.CrossProjectFilter{}, errors.New("epic_id must be a UUID or \"unassigned\"")
			}
			filter.EpicID = &id
		}
	}

	for _, v := range r.URL.Query()["projectId"] {
		id, err := uuid.Parse(v)
		if err != nil {
			return task.CrossProjectFilter{}, errors.New("projectId must be a UUID")
		}
		filter.ProjectIDs = append(filter.ProjectIDs, id)
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		if !isValidCrossProjectSort(v) {
			return task.CrossProjectFilter{}, errors.New("sort must be one of dueOn, priority, progress, size, updatedAt")
		}
		filter.Sort = v
	}

	filter.Limit = atoiOrZero(r.URL.Query().Get("limit"))

	assigneeID, unassigned, err := parseAssigneeFilter(r)
	if err != nil {
		return task.CrossProjectFilter{}, err
	}
	filter.AssigneeUserID, filter.AssigneeUnassigned = assigneeID, unassigned

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
		EpicID:                 req.EpicID,
		AssigneeGitlabUserID:   req.AssigneeGitlabUserID,
		AssigneeGitlabUsername: req.AssigneeGitlabUsername,
		AssigneeUserID:         req.AssigneeUserID,
		Labels:                 req.Labels,
		DueOn:                  req.DueOn,
		StartDate:              req.StartDate,
		Priority:               req.Priority,
		Progress:               req.Progress,
		Size:                   req.Size,
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// handleBulkCreateTasks creates a batch of tasks and the dependencies
// between them in one all-or-nothing transaction (issue #201): either every
// task, dependency and outbox sync job commits, or none of it does. See
// task.Service.BulkCreate.
func (s *Server) handleBulkCreateTasks(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req bulkCreateTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	params := task.BulkCreateParams{
		Tasks:        make([]task.BulkTaskParams, len(req.Tasks)),
		Dependencies: make([]task.BulkDependencyParams, len(req.Dependencies)),
	}
	for i, t := range req.Tasks {
		bt := task.BulkTaskParams{
			Ref: t.Ref,
			CreateParams: task.CreateParams{
				Title:          t.Title,
				Description:    t.Description,
				BacklogID:      t.BacklogID,
				EpicID:         t.EpicID,
				AssigneeUserID: t.AssigneeUserID,
				Labels:         t.Labels,
				DueOn:          t.DueOn,
				StartDate:      t.StartDate,
				Priority:       t.Priority,
				Size:           t.Size,
			},
		}
		if t.AIContext != nil {
			bt.AIContext = &task.AIContextParams{
				AcceptanceCriteria: t.AIContext.AcceptanceCriteria,
				AIContext:          t.AIContext.AIContext,
			}
		}
		params.Tasks[i] = bt
	}
	for i, d := range req.Dependencies {
		params.Dependencies[i] = task.BulkDependencyParams{
			PredecessorRef: d.PredecessorRef,
			SuccessorRef:   d.SuccessorRef,
		}
	}

	result, err := s.tasks.BulkCreate(r.Context(), u.ID, projectID, params)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
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
		EpicID:                 req.EpicID,
		AssigneeGitlabUserID:   req.AssigneeGitlabUserID,
		AssigneeGitlabUsername: req.AssigneeGitlabUsername,
		AssigneeUserID:         req.AssigneeUserID,
		Labels:                 req.Labels,
		DueOn:                  req.DueOn,
		StartDate:              req.StartDate,
		Priority:               req.Priority,
		Progress:               req.Progress,
		Size:                   req.Size,
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
	})
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, aiContext)
}

// writeTaskError maps a task domain error to its HTTP response. A
// *task.BulkError (from BulkCreate) is unwrapped first so the offending
// ref, if any, is appended to the message; the inner error still drives the
// status code and error code exactly as it would outside a bulk request.
func writeTaskError(w http.ResponseWriter, err error) {
	var bulkErr *task.BulkError
	if errors.As(err, &bulkErr) {
		status, code, message := taskErrorDetails(bulkErr.Err)
		if bulkErr.Ref != "" {
			message = fmt.Sprintf("%s (ref %q)", message, bulkErr.Ref)
		}
		writeError(w, status, code, message)
		return
	}
	status, code, message := taskErrorDetails(err)
	writeError(w, status, code, message)
}

// taskErrorDetails maps a task domain error to the HTTP status, error code
// and message writeTaskError (and writeTaskError's bulk-request path)
// responds with.
func taskErrorDetails(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, task.ErrInvalidTitle):
		return http.StatusBadRequest, "invalid_title", "title must be 1-255 characters"
	case errors.Is(err, task.ErrInvalidSize):
		return http.StatusBadRequest, "invalid_size", "size must be one of xs, s, m, l, xl"
	case errors.Is(err, task.ErrInvalidPriority):
		return http.StatusBadRequest, "invalid_priority", "priority must be one of low, medium, high, urgent"
	case errors.Is(err, task.ErrInvalidProgress):
		return http.StatusBadRequest, "invalid_progress", "progress must be one of not_started, in_progress, on_hold, done"
	case errors.Is(err, task.ErrBacklogNotInProject):
		return http.StatusBadRequest, "invalid_backlog", "backlog belongs to a different project"
	case errors.Is(err, task.ErrAssigneeNotMember):
		return http.StatusBadRequest, "invalid_assignee", "assignee must be a member of the project"
	case errors.Is(err, task.ErrAIContextFieldTooLong):
		return http.StatusBadRequest, "ai_context_field_too_long", "AI context fields must be at most 20000 characters"
	case errors.Is(err, task.ErrSyncNotFailed):
		return http.StatusConflict, "sync_not_failed", "gitlab sync is not currently failed"
	case errors.Is(err, task.ErrBulkTasksEmpty):
		return http.StatusBadRequest, "bulk_tasks_empty", "tasks must include at least one task"
	case errors.Is(err, task.ErrBulkTooManyTasks):
		return http.StatusBadRequest, "bulk_too_many_tasks", "tasks must not exceed 100"
	case errors.Is(err, task.ErrBulkRefEmpty):
		return http.StatusBadRequest, "bulk_ref_empty", "every task must have a non-empty ref"
	case errors.Is(err, task.ErrBulkDuplicateRef):
		return http.StatusBadRequest, "bulk_duplicate_ref", "task refs must be unique within the request"
	case errors.Is(err, task.ErrBulkUnknownRef):
		return http.StatusBadRequest, "bulk_unknown_ref", "dependency references a ref not present in tasks"
	case errors.Is(err, task.ErrBulkSelfDependency):
		return http.StatusBadRequest, "bulk_self_dependency", "a task cannot depend on itself"
	case errors.Is(err, task.ErrBulkCyclicDependency):
		return http.StatusBadRequest, "bulk_cyclic_dependency", "would create a cyclic dependency"
	case errors.Is(err, task.ErrNotFound):
		return http.StatusNotFound, "not_found", "task not found"
	case errors.Is(err, task.ErrForbidden):
		return http.StatusForbidden, "forbidden", "insufficient project role"
	default:
		slog.Error("task request", "error", err)
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}
