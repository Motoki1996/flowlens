package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/projectmember"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type addProjectMemberRequest struct {
	Identifier string `json:"identifier"`
	Role       string `json:"role"`
}

type updateProjectMemberRequest struct {
	Role string `json:"role"`
}

// memberUserIDFromURL parses the {userID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown member.
func memberUserIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListProjectMembers returns every member of the project, open to any
// project member (not just the owner) — a Backlog/Epic/Task assignee picker
// needs this list to populate its options regardless of the caller's role.
func (s *Server) handleListProjectMembers(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	members, err := s.projectMembers.List(r.Context(), u.ID, projectID)
	if err != nil {
		writeProjectMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

// handleSearchProjectMemberCandidates backs the invite form's user picker
// (issue #140), owner-only. It is deliberately project-scoped rather than a
// general /users/search: the candidate set is defined relative to a project
// the caller owns, and mounting it here means the owner check is the same one
// every other membership route performs.
func (s *Server) handleSearchProjectMemberCandidates(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	candidates, err := s.projectMembers.SearchCandidates(r.Context(), u.ID, projectID, r.URL.Query().Get("q"))
	if err != nil {
		writeProjectMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, candidates)
}

// handleAddProjectMember adds an existing user (by username or email) to the
// project, owner-only.
func (s *Server) handleAddProjectMember(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req addProjectMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	member, err := s.projectMembers.Add(r.Context(), u.ID, projectID, req.Identifier, req.Role)
	if err != nil {
		writeProjectMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, member)
}

// handleUpdateProjectMember changes a member's role, owner-only.
func (s *Server) handleUpdateProjectMember(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	userID, ok := memberUserIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}

	var req updateProjectMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	member, err := s.projectMembers.UpdateRole(r.Context(), u.ID, projectID, userID, req.Role)
	if err != nil {
		writeProjectMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

// handleRemoveProjectMember removes a member from the project, owner-only.
func (s *Server) handleRemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	userID, ok := memberUserIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}

	if err := s.projectMembers.Remove(r.Context(), u.ID, projectID, userID); err != nil {
		writeProjectMemberError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProjectMemberError maps a projectmember domain error to its HTTP
// response.
func writeProjectMemberError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectmember.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid_role", "role must be owner, member, or viewer")
	case errors.Is(err, projectmember.ErrSelfRole):
		writeError(w, http.StatusBadRequest, "self_role_change", "you cannot change your own role")
	case errors.Is(err, projectmember.ErrSelfRemove):
		writeError(w, http.StatusBadRequest, "self_remove", "you cannot remove yourself from the project")
	case errors.Is(err, projectmember.ErrLastOwner):
		writeError(w, http.StatusBadRequest, "last_owner", "the project's owner cannot be demoted or removed")
	case errors.Is(err, projectmember.ErrAlreadyMember):
		writeError(w, http.StatusConflict, "already_member", "this user is already a project member")
	case errors.Is(err, projectmember.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found")
	case errors.Is(err, projectmember.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, projectmember.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("project member request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
