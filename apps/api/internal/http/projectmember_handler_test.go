package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListProjectMembers(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	member := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(pID, member.ID, "member")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+pID.String()+"/members", nil, session)
	require.Equal(t, http.StatusOK, rec.Code)
	var members []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &members))
	require.Len(t, members, 2)
	assert.NotContains(t, rec.Body.String(), "email", "member responses must never carry an email field")
}

func TestHandleAddProjectMember(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	q.SeedUser("hubot", "hubot@example.com")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+pID.String()+"/members",
		addProjectMemberRequest{Identifier: "hubot", Role: "member"}, session)
	require.Equal(t, http.StatusCreated, rec.Code)
	var added map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &added))
	assert.Equal(t, "hubot", added["username"])
	assert.Equal(t, "member", added["role"])
}

func TestHandleAddProjectMember_ReturnsNotFoundForUnknownUser(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+pID.String()+"/members",
		addProjectMemberRequest{Identifier: "nobody", Role: "member"}, session)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleAddProjectMember_ReturnsForbiddenForMemberOrViewer(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, _ := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	q.SeedUser("hubot", "hubot@example.com")

	member := q.SeedUser("member", "member@example.com")
	q.SeedProjectMember(pID, member.ID, "member")
	memberSession, err := s.sessions.Create(context.Background(), member.ID)
	require.NoError(t, err)

	viewer := q.SeedUser("viewer", "viewer@example.com")
	q.SeedProjectMember(pID, viewer.ID, "viewer")
	viewerSession, err := s.sessions.Create(context.Background(), viewer.ID)
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		session string
	}{
		{"member", memberSession},
		{"viewer", viewerSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+pID.String()+"/members",
				addProjectMemberRequest{Identifier: "hubot", Role: "viewer"}, tc.session)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

func TestHandleUpdateProjectMember(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(pID, target.ID, "viewer")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+pID.String()+"/members/"+target.ID.String(),
		updateProjectMemberRequest{Role: "member"}, session)
	require.Equal(t, http.StatusOK, rec.Code)
	var updated map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, "member", updated["role"])
}

func TestHandleUpdateProjectMember_RejectsSelfRoleChange(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+pID.String()+"/members/"+ownerID.String(),
		updateProjectMemberRequest{Role: "member"}, session)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUpdateProjectMember_RejectsDemotingTheDesignatedOwner(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, _ := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	otherOwner := q.SeedUser("otherowner", "otherowner@example.com")
	q.SeedProjectMember(pID, otherOwner.ID, "owner")
	otherOwnerSession, err := s.sessions.Create(context.Background(), otherOwner.ID)
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+pID.String()+"/members/"+ownerID.String(),
		updateProjectMemberRequest{Role: "member"}, otherOwnerSession)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUpdateProjectMember_ReturnsForbiddenForMemberOrViewer(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, _ := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(pID, target.ID, "viewer")

	member := q.SeedUser("member", "member@example.com")
	q.SeedProjectMember(pID, member.ID, "member")
	memberSession, err := s.sessions.Create(context.Background(), member.ID)
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+pID.String()+"/members/"+target.ID.String(),
		updateProjectMemberRequest{Role: "member"}, memberSession)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleRemoveProjectMember(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(pID, target.ID, "viewer")

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+pID.String()+"/members/"+target.ID.String(), nil, session)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+pID.String()+"/members", nil, session)
	require.Equal(t, http.StatusOK, listRec.Code)
	var members []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &members))
	require.Len(t, members, 1)
}

func TestHandleRemoveProjectMember_RejectsRemovingTheDesignatedOwner(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, _ := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	otherOwner := q.SeedUser("otherowner", "otherowner@example.com")
	q.SeedProjectMember(pID, otherOwner.ID, "owner")
	otherOwnerSession, err := s.sessions.Create(context.Background(), otherOwner.ID)
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+pID.String()+"/members/"+ownerID.String(), nil, otherOwnerSession)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleRemoveProjectMember_ReturnsForbiddenForMemberOrViewer(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, _ := loginSession(t, s, q)
	pID := q.SeedProject(ownerID, "Alpha").ID
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(pID, target.ID, "viewer")

	viewer := q.SeedUser("viewer", "viewer@example.com")
	q.SeedProjectMember(pID, viewer.ID, "viewer")
	viewerSession, err := s.sessions.Create(context.Background(), viewer.ID)
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+pID.String()+"/members/"+target.ID.String(), nil, viewerSession)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
