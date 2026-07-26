package gitlab_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer wires an httptest server whose handler is provided per test,
// and an HTTPClient pointed at it.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*gitlab.HTTPClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return gitlab.NewHTTPClient(srv.URL), srv
}

func TestHTTPClient_GetAuthenticatedUser(t *testing.T) {
	tests := []struct {
		name       string
		respStatus int
		respBody   string
		wantErr    bool
		wantUser   *gitlab.User
	}{
		{
			name:       "ok",
			respStatus: http.StatusOK,
			respBody:   `{"id":1,"username":"octocat","name":"Octo Cat","avatar_url":"https://example.com/a.png"}`,
			wantUser:   &gitlab.User{ID: 1, Username: "octocat", Name: "Octo Cat", AvatarURL: "https://example.com/a.png"},
		},
		{
			name:       "unauthorized",
			respStatus: http.StatusUnauthorized,
			respBody:   `{"message":"401 Unauthorized"}`,
			wantErr:    true,
		},
		{
			name:       "not found",
			respStatus: http.StatusNotFound,
			respBody:   `{"message":"404 Not Found"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "secret-token", r.Header.Get("PRIVATE-TOKEN"))
				assert.NotContains(t, r.URL.String(), "secret-token")
				w.WriteHeader(tt.respStatus)
				_, _ = w.Write([]byte(tt.respBody))
			})

			user, err := client.GetAuthenticatedUser(context.Background(), "secret-token")
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *gitlab.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tt.respStatus, apiErr.StatusCode)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUser, user)
		})
	}
}

func TestHTTPClient_ListIssues_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		respStatus int
		respBody   string
		wantErr    bool
		wantLen    int
	}{
		{
			name:       "ok",
			respStatus: http.StatusOK,
			respBody:   `[{"id":1,"iid":1,"project_id":42,"title":"first"},{"id":2,"iid":2,"project_id":42,"title":"second"}]`,
			wantLen:    2,
		},
		{
			name:       "unauthorized",
			respStatus: http.StatusUnauthorized,
			respBody:   `{"message":"401 Unauthorized"}`,
			wantErr:    true,
		},
		{
			name:       "not found",
			respStatus: http.StatusNotFound,
			respBody:   `{"message":"404 Project Not Found"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v4/projects/42/issues", r.URL.Path)
				w.WriteHeader(tt.respStatus)
				_, _ = w.Write([]byte(tt.respBody))
			})

			issues, _, err := client.ListIssues(context.Background(), "secret-token", 42, gitlab.ListIssuesOptions{})
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *gitlab.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tt.respStatus, apiErr.StatusCode)
				return
			}
			require.NoError(t, err)
			assert.Len(t, issues, tt.wantLen)
		})
	}
}

func TestHTTPClient_ListIssues_SendsLabelsStateAndUpdatedAfterAsQuery(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "bug,urgent", q.Get("labels"))
		assert.Equal(t, "opened", q.Get("state"))
		assert.NotEmpty(t, q.Get("updated_after"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	after, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	_, _, err = client.ListIssues(context.Background(), "secret-token", 42, gitlab.ListIssuesOptions{
		Labels:       []string{"bug", "urgent"},
		State:        "opened",
		UpdatedAfter: &after,
	})
	require.NoError(t, err)
}

func TestHTTPClient_ListMemberProjects_Paginates(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "true", r.URL.Query().Get("membership"))
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":3,"name":"third"}]`))
			return
		}
		w.Header().Set("X-Next-Page", "2")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1,"name":"first"},{"id":2,"name":"second"}]`))
	})

	page1, info1, err := client.ListMemberProjects(context.Background(), "secret-token", gitlab.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.Equal(t, 2, info1.NextPage)

	page2, info2, err := client.ListMemberProjects(context.Background(), "secret-token", gitlab.ListOptions{Page: 2})
	require.NoError(t, err)
	assert.Len(t, page2, 1)
	assert.Equal(t, 0, info2.NextPage)
}

func TestHTTPClient_CreateIssue_SendsPayloadAndReturnsCreated(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var got gitlab.IssuePayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		assert.Equal(t, "New issue", got.Title)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9,"iid":1,"project_id":42,"title":"New issue"}`))
	})

	issue, err := client.CreateIssue(context.Background(), "secret-token", 42, gitlab.IssuePayload{Title: "New issue"})
	require.NoError(t, err)
	assert.Equal(t, "New issue", issue.Title)
}

func TestHTTPClient_DeleteProjectHook(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v4/projects/42/hooks/7", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	err := client.DeleteProjectHook(context.Background(), "secret-token", 42, 7)
	require.NoError(t, err)
}

func TestHTTPClient_RetriesOnTransientErrors(t *testing.T) {
	attempts := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"temporarily unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"username":"octocat"}`))
	})

	user, err := client.GetAuthenticatedUser(context.Background(), "secret-token")
	require.NoError(t, err)
	assert.Equal(t, "octocat", user.Username)
	assert.Equal(t, 2, attempts)
}

func TestHTTPClient_TokenNeverAppearsInRequestURL(t *testing.T) {
	const token = "s3cr3t-pat"
	var seenPath string
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		assert.Equal(t, token, r.Header.Get("PRIVATE-TOKEN"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"username":"octocat"}`))
	})

	_, err := client.GetAuthenticatedUser(context.Background(), token)
	require.NoError(t, err)
	assert.False(t, strings.Contains(seenPath, token))
}
