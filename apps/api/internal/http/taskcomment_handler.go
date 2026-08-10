package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/taskcomment"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createTaskCommentRequest struct {
	Body string `json:"body"`
}

// taskCommentIDFromURL parses the {commentID} path parameter. A malformed ID
// is reported as "not found" so it is indistinguishable from an unknown one.
func taskCommentIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListTaskComments returns a task's activity log, oldest first. A
// bearer caller is scoped through its token's own project
// (taskcomment.Service.ListForProject); a session caller is scoped through
// task.Service's own project ownership check (taskcomment.Service.List).
func (s *Server) handleListTaskComments(w http.ResponseWriter, r *http.Request) {
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	var (
		comments []taskcomment.TaskComment
		err      error
	)
	if projectID, ok := tokenProjectFromContext(r.Context()); ok {
		comments, err = s.taskComments.ListForProject(r.Context(), projectID, taskID)
	} else {
		u, _ := userFromContext(r.Context())
		comments, err = s.taskComments.List(r.Context(), u.ID, taskID)
	}
	if err != nil {
		writeTaskCommentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

// handleCreateTaskComment posts to a task's activity log: a human author
// (author_kind "user") for a session caller, an agent author (author_kind
// "agent") identified by the token itself for a bearer caller.
func (s *Server) handleCreateTaskComment(w http.ResponseWriter, r *http.Request) {
	taskID, ok := taskIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	var req createTaskCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	var (
		comment taskcomment.TaskComment
		err     error
	)
	if ts, ok := tokenScopeFromContext(r.Context()); ok {
		comment, err = s.taskComments.CreateForProject(r.Context(), ts.ProjectID, ts.TokenID, taskID, req.Body)
	} else {
		u, _ := userFromContext(r.Context())
		comment, err = s.taskComments.Create(r.Context(), u.ID, taskID, req.Body)
	}
	if err != nil {
		writeTaskCommentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

// handleDeleteTaskComment removes one comment — only the comment's own
// author may delete it (taskcomment.Service.Delete/DeleteForToken), session
// or bearer respectively.
func (s *Server) handleDeleteTaskComment(w http.ResponseWriter, r *http.Request) {
	commentID, ok := taskCommentIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "comment not found")
		return
	}

	var err error
	if ts, ok := tokenScopeFromContext(r.Context()); ok {
		err = s.taskComments.DeleteForToken(r.Context(), ts.TokenID, commentID)
	} else {
		u, _ := userFromContext(r.Context())
		err = s.taskComments.Delete(r.Context(), u.ID, commentID)
	}
	if err != nil {
		writeTaskCommentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeTaskCommentError maps a taskcomment domain error to its HTTP
// response.
func writeTaskCommentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, taskcomment.ErrInvalidBody):
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
	case errors.Is(err, taskcomment.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, taskcomment.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "you can only delete your own comments")
	default:
		slog.Error("task comment request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
