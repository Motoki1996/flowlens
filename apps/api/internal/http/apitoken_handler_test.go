package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCreateAPIToken_ReturnsRawTokenOnlyOnce(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/api-tokens",
		createAPITokenRequest{Name: "CI bot"}, session)

	require.Equal(t, http.StatusCreated, rec.Code)
	var created createAPITokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotEmpty(t, created.Token)
	assert.Equal(t, "CI bot", created.Name)

	// The raw token must never appear in the list response.
	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/api-tokens", nil, session)
	require.Equal(t, http.StatusOK, listRec.Code)
	assert.NotContains(t, listRec.Body.String(), created.Token, "the plaintext token must never appear outside the create response")
	assert.NotContains(t, listRec.Body.String(), "token\"", "the list response must not carry a token field at all")
}

func TestHandleCreateAPIToken_RejectsInvalidName(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/api-tokens",
		createAPITokenRequest{Name: "   "}, session)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreateAPIToken_ReturnsNotFoundForForeignProject(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	ownerToken, err := s.sessions.Create(context.Background(), owner.ID)
	require.NoError(t, err)

	other := q.SeedUser("intruder", "intruder@example.com")
	id := q.SeedProject(other.ID, "Alpha").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/api-tokens",
		createAPITokenRequest{Name: "CI bot"}, ownerToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListAPITokens(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	require.Equal(t, http.StatusCreated,
		doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/api-tokens", createAPITokenRequest{Name: "CI bot"}, session).Code)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/api-tokens", nil, session)
	require.Equal(t, http.StatusOK, rec.Code)
	var tokens []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tokens))
	require.Len(t, tokens, 1)
	assert.Equal(t, "CI bot", tokens[0]["name"])
}

func TestHandleDeleteAPIToken(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/api-tokens", createAPITokenRequest{Name: "CI bot"}, session)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created createAPITokenResponse
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	deleteRec := doRequest(t, s, http.MethodDelete, "/api/v1/api-tokens/"+created.ID.String(), nil, session)
	assert.Equal(t, http.StatusNoContent, deleteRec.Code)

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/api-tokens", nil, session)
	require.Equal(t, http.StatusOK, listRec.Code)
	var tokens []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &tokens))
	assert.Empty(t, tokens)
}

func TestHandleDeleteAPIToken_ReturnsNotFoundForForeignToken(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerSession := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()
	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/api-tokens", createAPITokenRequest{Name: "CI bot"}, ownerSession)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created createAPITokenResponse
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	other := q.SeedUser("intruder", "intruder@example.com")
	otherSession, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/api-tokens/"+created.ID.String(), nil, otherSession)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
