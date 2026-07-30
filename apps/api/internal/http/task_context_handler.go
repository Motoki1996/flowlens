package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
)

// parseTaskContextListFilter reads the ?backlog_id=, ?status=,
// ?updated_since=, ?page= and ?per_page= query parameters for the bulk
// AI-facing context endpoint. backlog_id accepts a UUID — unlike
// parseTaskListFilter, "unassigned" is not supported here, since the
// AI-facing bulk view has no use for it yet. updated_since accepts an
// RFC 3339 timestamp.
func parseTaskContextListFilter(r *http.Request) (task.ContextListFilter, error) {
	q := r.URL.Query()
	var filter task.ContextListFilter

	if v := q.Get("backlog_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return task.ContextListFilter{}, errors.New("backlog_id must be a UUID")
		}
		filter.BacklogID = &id
	}

	if v := q.Get("status"); v != "" {
		if v != task.StatusOpen && v != task.StatusClosed {
			return task.ContextListFilter{}, errors.New("status must be \"open\" or \"closed\"")
		}
		filter.Status = v
	}

	if v := q.Get("updated_since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return task.ContextListFilter{}, errors.New("updated_since must be an RFC 3339 timestamp")
		}
		filter.UpdatedSince = &t
	}

	filter.Page = atoiOrZero(q.Get("page"))
	filter.PerPage = atoiOrZero(q.Get("per_page"))

	return filter, nil
}

// handleGetTaskContext returns one task's combined AI-facing context
// (task.Context): mirrored task fields, GitLab sync state, and AI context in
// one payload — see docs/plans/issue-sync.md's "AI-facing" API surface.
// Mounted behind requireAuthOrBearer: a session caller is scoped through the
// task's project owner (task.Service.Context); a bearer caller is scoped
// through its token's own project (task.Service.ContextForProject), with no
// {projectID} in this route's URL to check against.
func (s *Server) handleGetTaskContext(w http.ResponseWriter, r *http.Request) {
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	var (
		c   task.Context
		err error
	)
	if projectID, ok := tokenProjectFromContext(r.Context()); ok {
		c, err = s.tasks.ContextForProject(r.Context(), projectID, taskID)
	} else {
		u, _ := userFromContext(r.Context())
		c, err = s.tasks.Context(r.Context(), u.ID, taskID)
	}
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleListTaskContexts returns a page of a project's combined AI-facing
// task contexts, matching the backlog_id/status/updated_since query filters.
// Mounted behind requireAuthOrBearer + requireTokenProjectMatch: a bearer
// caller's {projectID} was already verified to match its token, so no
// ownership check is needed here; a session caller is scoped through
// task.Service.ListContext's own project ownership check.
func (s *Server) handleListTaskContexts(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	filter, err := parseTaskContextListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}

	var page task.ContextPage
	if _, ok := tokenProjectFromContext(r.Context()); ok {
		page, err = s.tasks.ListContextForProject(r.Context(), projectID, filter)
	} else {
		u, _ := userFromContext(r.Context())
		page, err = s.tasks.ListContext(r.Context(), u.ID, projectID, filter)
	}
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Tasks    []task.Context `json:"tasks"`
		NextPage int            `json:"nextPage"`
	}{Tasks: page.Tasks, NextPage: page.NextPage})
}
