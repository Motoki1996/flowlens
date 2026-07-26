// Package gitlab wraps the parts of the GitLab CE REST API that FlowLens
// needs. It is defined as an interface so a fake implementation can be used
// in tests and so future concerns (pagination, rate limiting, retries) live
// behind a single seam. It is not wired into any handler yet — a later
// phase uses it to sync merge requests once a user connects a GitLab
// personal access token.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// User is the subset of a GitLab user that FlowLens stores.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// Client is the seam over the GitLab CE API. Later phases add project and
// merge-request methods here; the fake in fake.go implements the same set.
type Client interface {
	// GetAuthenticatedUser returns the user that owns the personal access token.
	GetAuthenticatedUser(ctx context.Context, personalAccessToken string) (*User, error)
}

// IssuePayload is the exact set of fields FlowLens pushes to a GitLab CE
// issue on task create/update (docs/plans/issue-sync.md phase 4+). It is
// built only from a task's mirrored fields (title, description, labels,
// assignee, due date) — never from task_ai_contexts, which is app-only and
// must never reach GitLab. See task.BuildGitlabIssuePayload, the single seam
// that constructs a value of this type.
type IssuePayload struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Labels      []string   `json:"labels"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	AssigneeIDs []int64    `json:"assignee_ids,omitempty"`
}

// APIError represents a non-2xx response from GitLab.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gitlab: unexpected status %d: %s", e.StatusCode, e.Body)
}

// HTTPClient is the real GitLab CE API client. GitLab CE is self-hosted, so
// the base URL is caller-supplied rather than a fixed constant.
type HTTPClient struct {
	baseURL string
	http    *http.Client
}

// NewHTTPClient builds a client with a sensible timeout, pointed at a
// GitLab CE instance's base URL (e.g. "https://gitlab.example.com").
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *HTTPClient) GetAuthenticatedUser(ctx context.Context, personalAccessToken string) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v4/user", nil)
	if err != nil {
		return nil, fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", personalAccessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("gitlab: decode user: %w", err)
	}
	if user.ID == 0 || user.Username == "" {
		return nil, fmt.Errorf("gitlab: user response missing required fields")
	}
	return &user, nil
}

func readAPIError(resp *http.Response) error {
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	return &APIError{StatusCode: resp.StatusCode, Body: string(buf[:n])}
}
