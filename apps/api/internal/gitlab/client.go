// Package gitlab wraps the parts of the GitLab CE REST API that FlowLens
// needs. It is defined as an interface so a fake implementation can be used
// in tests and so future concerns (pagination, rate limiting, retries) live
// behind a single seam. internal/linkedproject wires up the webhook methods
// (issue #18); the issue/member methods are reserved for the sync engine
// phases in docs/plans/issue-sync.md.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// User is the subset of a GitLab user that FlowLens stores.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// Project is the subset of a GitLab project FlowLens stores when a user
// selects it to sync.
type Project struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
}

// Label is a GitLab project label, for populating a task's label picker.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Issue is the subset of a GitLab issue FlowLens reads and writes.
type Issue struct {
	ID          int64     `json:"id"`
	IID         int64     `json:"iid"`
	ProjectID   int64     `json:"project_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	Labels      []string  `json:"labels"`
	DueDate     string    `json:"due_date"`
	Assignees   []User    `json:"assignees"`
	WebURL      string    `json:"web_url"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectHook is a GitLab CE project webhook. Token is write-only: GitLab CE
// never echoes it back in a response body, so it is empty on List/Get.
type ProjectHook struct {
	ID                  int64  `json:"id"`
	URL                 string `json:"url"`
	Token               string `json:"token,omitempty"`
	PushEvents          bool   `json:"push_events"`
	MergeRequestsEvents bool   `json:"merge_requests_events"`
	IssuesEvents        bool   `json:"issues_events"`
	PipelineEvents      bool   `json:"pipeline_events"`
	NoteEvents          bool   `json:"note_events"`
}

// Note is the subset of a GitLab issue note (comment) FlowLens reads and
// writes (#104). System is true for GitLab-generated notes ("changed the
// description", "closed", ...) rather than a user's own comment, and is
// never mirrored into task_comments.
type Note struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	System    bool      `json:"system"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateNotePayload is the exact set of fields FlowLens pushes when posting
// a task comment to a GitLab issue as a note.
type CreateNotePayload struct {
	Body string `json:"body"`
}

// ListOptions is the pagination input shared by every list endpoint. Search,
// when set, is sent as GitLab's `search` query parameter (used by
// ListMemberProjects to let a user filter which project to link).
type ListOptions struct {
	Page    int
	PerPage int
	Search  string
}

func (o ListOptions) addTo(q url.Values) {
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(o.PerPage))
	}
	if o.Search != "" {
		q.Set("search", o.Search)
	}
}

// ListIssuesOptions filters an issue listing for initial import and resync.
type ListIssuesOptions struct {
	ListOptions
	// Labels, when non-empty, is sent as a comma-separated GitLab label filter.
	Labels []string
	// UpdatedAfter, when set, limits results to issues touched since this time.
	UpdatedAfter *time.Time
	// State is one of "opened", "closed", "all". Empty means GitLab's default.
	State string
}

func (o ListIssuesOptions) toQuery() url.Values {
	q := url.Values{}
	o.addTo(q)
	if len(o.Labels) > 0 {
		q.Set("labels", strings.Join(o.Labels, ","))
	}
	if o.UpdatedAfter != nil {
		q.Set("updated_after", o.UpdatedAfter.UTC().Format(time.RFC3339))
	}
	if o.State != "" {
		q.Set("state", o.State)
	}
	return q
}

// PageInfo reports GitLab's `X-Next-Page` pagination header. NextPage is 0
// once the last page has been reached.
type PageInfo struct {
	NextPage int
}

// Client is the seam over the GitLab CE API. The fake in fake.go implements
// the same set for tests.
type Client interface {
	// GetAuthenticatedUser returns the user that owns the personal access token.
	GetAuthenticatedUser(ctx context.Context, personalAccessToken string) (*User, error)

	// ListMemberProjects lists projects the token's user is a member of, for
	// picking a project to connect.
	ListMemberProjects(ctx context.Context, personalAccessToken string, opts ListOptions) ([]Project, PageInfo, error)
	// GetProject fetches a single project by its GitLab ID.
	GetProject(ctx context.Context, personalAccessToken string, projectID int64) (*Project, error)
	// ListProjectMembers lists a project's members, for assignee selection.
	ListProjectMembers(ctx context.Context, personalAccessToken string, projectID int64, opts ListOptions) ([]User, PageInfo, error)
	// ListProjectLabels lists a project's existing labels, for label selection.
	ListProjectLabels(ctx context.Context, personalAccessToken string, projectID int64, opts ListOptions) ([]Label, PageInfo, error)

	// ListIssues lists a project's issues, for initial import and resync.
	ListIssues(ctx context.Context, personalAccessToken string, projectID int64, opts ListIssuesOptions) ([]Issue, PageInfo, error)
	// GetIssue fetches a single issue by its project-scoped IID.
	GetIssue(ctx context.Context, personalAccessToken string, projectID, issueIID int64) (*Issue, error)
	// CreateIssue pushes a new issue to GitLab.
	CreateIssue(ctx context.Context, personalAccessToken string, projectID int64, payload IssuePayload) (*Issue, error)
	// UpdateIssue applies a partial update to an existing issue.
	UpdateIssue(ctx context.Context, personalAccessToken string, projectID, issueIID int64, payload UpdateIssuePayload) (*Issue, error)
	// CreateNote posts a new note (comment) on an issue.
	CreateNote(ctx context.Context, personalAccessToken string, projectID, issueIID int64, payload CreateNotePayload) (*Note, error)

	// ListProjectHooks lists a project's webhooks.
	ListProjectHooks(ctx context.Context, personalAccessToken string, projectID int64) ([]ProjectHook, error)
	// CreateProjectHook registers a new webhook on a project.
	CreateProjectHook(ctx context.Context, personalAccessToken string, projectID int64, hook ProjectHook) (*ProjectHook, error)
	// UpdateProjectHook replaces an existing webhook's configuration.
	UpdateProjectHook(ctx context.Context, personalAccessToken string, projectID, hookID int64, hook ProjectHook) (*ProjectHook, error)
	// DeleteProjectHook removes a webhook from a project.
	DeleteProjectHook(ctx context.Context, personalAccessToken string, projectID, hookID int64) error
}

// IssuePayload is the exact set of fields FlowLens pushes to a GitLab CE
// issue on task create (docs/plans/issue-sync.md phase 4+). It is built only
// from a task's mirrored fields (title, description, labels, assignee, due
// date) — never from task_ai_contexts, which is app-only and must never
// reach GitLab. See task.BuildGitlabIssuePayload, the single seam that
// constructs a value of this type.
type IssuePayload struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Labels      []string   `json:"labels"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	AssigneeIDs []int64    `json:"assignee_ids,omitempty"`
}

// UpdateIssuePayload is a partial update to an existing GitLab issue. Every
// field is optional so a caller can change just the state, just the
// assignee, and so on, without clobbering the rest.
type UpdateIssuePayload struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	AssigneeIDs []int64    `json:"assignee_ids,omitempty"`
	Labels      []string   `json:"labels,omitempty"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	// StateEvent is "close" or "reopen"; empty leaves the state untouched.
	StateEvent string `json:"state_event,omitempty"`
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

var _ Client = (*HTTPClient)(nil)

// NewHTTPClient builds a client with a sensible timeout, pointed at a
// GitLab CE instance's base URL (e.g. "https://gitlab.example.com").
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *HTTPClient) GetAuthenticatedUser(ctx context.Context, personalAccessToken string) (*User, error) {
	var user User
	if err := c.getOne(ctx, personalAccessToken, "/api/v4/user", &user); err != nil {
		return nil, err
	}
	if user.ID == 0 || user.Username == "" {
		return nil, fmt.Errorf("gitlab: user response missing required fields")
	}
	return &user, nil
}

func (c *HTTPClient) ListMemberProjects(ctx context.Context, personalAccessToken string, opts ListOptions) ([]Project, PageInfo, error) {
	q := url.Values{}
	q.Set("membership", "true")
	opts.addTo(q)

	var projects []Project
	page, err := c.getList(ctx, personalAccessToken, "/api/v4/projects", q, &projects)
	return projects, page, err
}

func (c *HTTPClient) GetProject(ctx context.Context, personalAccessToken string, projectID int64) (*Project, error) {
	var project Project
	if err := c.getOne(ctx, personalAccessToken, fmt.Sprintf("/api/v4/projects/%d", projectID), &project); err != nil {
		return nil, err
	}
	return &project, nil
}

func (c *HTTPClient) ListProjectMembers(ctx context.Context, personalAccessToken string, projectID int64, opts ListOptions) ([]User, PageInfo, error) {
	q := url.Values{}
	opts.addTo(q)

	var members []User
	page, err := c.getList(ctx, personalAccessToken, fmt.Sprintf("/api/v4/projects/%d/members/all", projectID), q, &members)
	return members, page, err
}

func (c *HTTPClient) ListProjectLabels(ctx context.Context, personalAccessToken string, projectID int64, opts ListOptions) ([]Label, PageInfo, error) {
	q := url.Values{}
	opts.addTo(q)

	var labels []Label
	page, err := c.getList(ctx, personalAccessToken, fmt.Sprintf("/api/v4/projects/%d/labels", projectID), q, &labels)
	return labels, page, err
}

func (c *HTTPClient) ListIssues(ctx context.Context, personalAccessToken string, projectID int64, opts ListIssuesOptions) ([]Issue, PageInfo, error) {
	var issues []Issue
	page, err := c.getList(ctx, personalAccessToken, fmt.Sprintf("/api/v4/projects/%d/issues", projectID), opts.toQuery(), &issues)
	return issues, page, err
}

func (c *HTTPClient) GetIssue(ctx context.Context, personalAccessToken string, projectID, issueIID int64) (*Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, issueIID)
	if err := c.getOne(ctx, personalAccessToken, path, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

func (c *HTTPClient) CreateIssue(ctx context.Context, personalAccessToken string, projectID int64, payload IssuePayload) (*Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/api/v4/projects/%d/issues", projectID)
	if err := c.mutateOne(ctx, http.MethodPost, path, personalAccessToken, payload, http.StatusCreated, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

func (c *HTTPClient) UpdateIssue(ctx context.Context, personalAccessToken string, projectID, issueIID int64, payload UpdateIssuePayload) (*Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, issueIID)
	if err := c.mutateOne(ctx, http.MethodPut, path, personalAccessToken, payload, http.StatusOK, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

func (c *HTTPClient) CreateNote(ctx context.Context, personalAccessToken string, projectID, issueIID int64, payload CreateNotePayload) (*Note, error) {
	var note Note
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes", projectID, issueIID)
	if err := c.mutateOne(ctx, http.MethodPost, path, personalAccessToken, payload, http.StatusCreated, &note); err != nil {
		return nil, err
	}
	return &note, nil
}

func (c *HTTPClient) ListProjectHooks(ctx context.Context, personalAccessToken string, projectID int64) ([]ProjectHook, error) {
	var hooks []ProjectHook
	_, err := c.getList(ctx, personalAccessToken, fmt.Sprintf("/api/v4/projects/%d/hooks", projectID), nil, &hooks)
	return hooks, err
}

func (c *HTTPClient) CreateProjectHook(ctx context.Context, personalAccessToken string, projectID int64, hook ProjectHook) (*ProjectHook, error) {
	var created ProjectHook
	path := fmt.Sprintf("/api/v4/projects/%d/hooks", projectID)
	if err := c.mutateOne(ctx, http.MethodPost, path, personalAccessToken, hook, http.StatusCreated, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

func (c *HTTPClient) UpdateProjectHook(ctx context.Context, personalAccessToken string, projectID, hookID int64, hook ProjectHook) (*ProjectHook, error) {
	var updated ProjectHook
	path := fmt.Sprintf("/api/v4/projects/%d/hooks/%d", projectID, hookID)
	if err := c.mutateOne(ctx, http.MethodPut, path, personalAccessToken, hook, http.StatusOK, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (c *HTTPClient) DeleteProjectHook(ctx context.Context, personalAccessToken string, projectID, hookID int64) error {
	path := fmt.Sprintf("/api/v4/projects/%d/hooks/%d", projectID, hookID)
	resp, err := c.doRequest(ctx, http.MethodDelete, path, personalAccessToken, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return readAPIError(resp)
	}
	return nil
}

// getOne performs a GET expected to return a single JSON object.
func (c *HTTPClient) getOne(ctx context.Context, token, path string, out any) error {
	resp, err := c.doRequest(ctx, http.MethodGet, path, token, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("gitlab: decode response: %w", err)
	}
	return nil
}

// getList performs a GET expected to return a JSON array, decoding the
// `X-Next-Page` response header into the returned PageInfo.
func (c *HTTPClient) getList(ctx context.Context, token, path string, query url.Values, out any) (PageInfo, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, token, query, nil)
	if err != nil {
		return PageInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return PageInfo{}, readAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return PageInfo{}, fmt.Errorf("gitlab: decode response: %w", err)
	}
	return PageInfo{NextPage: nextPageFromHeader(resp)}, nil
}

// mutateOne performs a POST/PUT expected to return wantStatus with a single
// JSON object body.
func (c *HTTPClient) mutateOne(ctx context.Context, method, path, token string, body any, wantStatus int, out any) error {
	resp, err := c.doRequest(ctx, method, path, token, nil, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		return readAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("gitlab: decode response: %w", err)
	}
	return nil
}

// maxAttempts bounds retries of 429/5xx responses: the first attempt plus
// three retries.
const maxAttempts = 4

// doRequest sends a single logical request, retrying on 429 and 5xx
// responses with a short linear backoff. The token is always sent via the
// PRIVATE-TOKEN header, never in the URL, so it cannot leak into access logs.
// The returned response's body is the caller's to close.
func (c *HTTPClient) doRequest(ctx context.Context, method, path, token string, query url.Values, body any) (*http.Response, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("gitlab: encode request body: %w", err)
		}
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryBackoff(attempt)):
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reqBody)
		if err != nil {
			return nil, fmt.Errorf("gitlab: build request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", token)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("gitlab: do request: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			lastErr = readAPIError(resp)
			continue
		}

		return resp, nil
	}
	return nil, lastErr
}

func retryBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 200 * time.Millisecond
}

func nextPageFromHeader(resp *http.Response) int {
	v := resp.Header.Get("X-Next-Page")
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func readAPIError(resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
}
