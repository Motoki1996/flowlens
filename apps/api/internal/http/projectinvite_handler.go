package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/projectinvite"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createProjectInviteRequest struct {
	Role string `json:"role"`
	// ExpiresInDays of 0 (or absent) means projectinvite.DefaultExpiryDays.
	ExpiresInDays int `json:"expiresInDays"`
}

type acceptProjectInviteRequest struct {
	Token string `json:"token"`
}

// createProjectInviteResponse carries the raw invite token alongside the
// record, exactly once — the same shape createAPITokenResponse uses, and
// for the same reason: only its hash is stored, so this response is the
// only chance to copy it.
type createProjectInviteResponse struct {
	projectinvite.Invite
	Token string `json:"token"`
}

// inviteIDFromURL parses the {inviteID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown
// invite.
func inviteIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "inviteID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleCreateProjectInvite issues an invite for the project, owner-only.
// The invite is what lets a person with no account at all join, so an
// instance that has closed registration (ALLOW_SIGNUP=false) can still
// onboard someone — see internal/projectinvite's package doc.
func (s *Server) handleCreateProjectInvite(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req createProjectInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	invite, rawToken, err := s.projectInvites.Create(r.Context(), u.ID, projectID, req.Role, req.ExpiresInDays)
	if err != nil {
		writeProjectInviteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createProjectInviteResponse{Invite: invite, Token: rawToken})
}

// handleListProjectInvites returns every invite issued for the project,
// owner-only — including spent and expired ones, so an owner can see who
// was let in and not only what is still outstanding.
func (s *Server) handleListProjectInvites(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	invites, err := s.projectInvites.List(r.Context(), u.ID, projectID)
	if err != nil {
		writeProjectInviteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invites)
}

// handleRevokeProjectInvite deletes an invite, owner-only.
func (s *Server) handleRevokeProjectInvite(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	inviteID, ok := inviteIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
		return
	}

	if err := s.projectInvites.Revoke(r.Context(), u.ID, inviteID); err != nil {
		writeProjectInviteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePreviewInvite resolves an invite token to the project and role it
// grants, for the acceptance screen to render. Unauthenticated on purpose:
// whoever holds the token is exactly who this is for, and they have no
// account yet in the case this feature exists to serve.
//
// It is rate-limited on the same limiter as login/signup, since an
// unauthenticated caller could otherwise probe tokens without bound.
func (s *Server) handlePreviewInvite(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.Allow(s.clientIP(r)) {
		writeTooManyRequests(w, authRateLimitWindow)
		return
	}

	preview, err := s.projectInvites.Preview(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeProjectInviteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// handleAcceptInvite spends an invite for the already-authenticated caller
// — the path for someone who was sent an invite but already has an
// account. Someone with no account instead sends the token with their
// signup (see handleSignup), which is the case this feature exists for.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req acceptProjectInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	accepted, err := s.projectInvites.Accept(r.Context(), req.Token, u.ID)
	if err != nil {
		writeProjectInviteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accepted)
}

// writeProjectInviteError maps projectinvite's sentinels to HTTP statuses.
// Every acceptance-path failure arrives as the single ErrInviteInvalid, so
// an unauthenticated caller cannot tell an expired invite from one that
// never existed.
func writeProjectInviteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectinvite.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid_role", "role must be owner, member, or viewer")
	case errors.Is(err, projectinvite.ErrInvalidExpiry):
		writeError(w, http.StatusBadRequest, "invalid_expiry", "expiry must be between 1 and 90 days")
	case errors.Is(err, projectinvite.ErrAlreadyMember):
		writeError(w, http.StatusConflict, "already_member", "you are already a member of this project")
	case errors.Is(err, projectinvite.ErrInviteInvalid):
		writeError(w, http.StatusNotFound, "invite_invalid", "this invite is invalid, expired or already used")
	case errors.Is(err, projectinvite.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, projectinvite.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("project invite request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
