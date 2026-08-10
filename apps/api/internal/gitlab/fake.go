package gitlab

import "context"

// Call records a single FakeClient method invocation, for tests that assert
// on outbound behaviour (e.g. "did sync call UpdateIssue with this payload")
// without a real HTTP server.
type Call struct {
	Method string
	Args   []any
}

// FakeClient is an in-memory Client for tests. Each method appends a Call to
// CallLog and returns the canned value/error configured for it, letting use
// cases be tested without network access. See docs/testing.md: prefer this
// stateful fake over a call-expectation mock.
type FakeClient struct {
	// AuthenticatedUser is returned by GetAuthenticatedUser when set.
	AuthenticatedUser *User
	// Err, when set, is returned by GetAuthenticatedUser.
	Err error
	// Calls counts GetAuthenticatedUser invocations.
	Calls int

	// Projects is returned by ListMemberProjects.
	Projects []Project
	// ProjectsErr, when set, is returned by ListMemberProjects.
	ProjectsErr error
	// Project is returned by GetProject.
	Project *Project
	// ProjectErr, when set, is returned by GetProject.
	ProjectErr error
	// Members is returned by ListProjectMembers.
	Members []User
	// MembersErr, when set, is returned by ListProjectMembers.
	MembersErr error
	// Labels is returned by ListProjectLabels.
	Labels []Label
	// LabelsErr, when set, is returned by ListProjectLabels.
	LabelsErr error

	// Issues is returned by ListIssues. Ignored once IssuesPages is set.
	Issues []Issue
	// IssuesPages, when set, overrides Issues so a test can exercise a
	// multi-page ListIssues walk (internal/projectsync): index 0 is
	// returned for opts.Page <= 1, index 1 for opts.Page == 2, and so on.
	// The last page reports PageInfo.NextPage 0; every earlier page reports
	// the following page number, mirroring GitLab's X-Next-Page header.
	IssuesPages [][]Issue
	// IssuesErr, when set, is returned by ListIssues.
	IssuesErr error
	// Issue is returned by GetIssue, CreateIssue, and UpdateIssue.
	Issue *Issue
	// GetIssueErr, when set, is returned by GetIssue.
	GetIssueErr error
	// CreateIssueErr, when set, is returned by CreateIssue.
	CreateIssueErr error
	// UpdateIssueErr, when set, is returned by UpdateIssue.
	UpdateIssueErr error

	// Note is returned by CreateNote when set. Otherwise CreateNote
	// synthesizes a Note with an auto-incrementing ID (nextNoteID): unlike
	// Issue (pushed once per task), a test may push several comments and
	// needs each to get a distinct GitLab note id.
	Note *Note
	// CreateNoteErr, when set, is returned by CreateNote.
	CreateNoteErr error
	nextNoteID    int64

	// Hooks is returned by ListProjectHooks.
	Hooks []ProjectHook
	// HooksErr, when set, is returned by ListProjectHooks.
	HooksErr error
	// Hook is returned by CreateProjectHook and UpdateProjectHook.
	Hook *ProjectHook
	// CreateHookErr, when set, is returned by CreateProjectHook.
	CreateHookErr error
	// UpdateHookErr, when set, is returned by UpdateProjectHook.
	UpdateHookErr error
	// DeleteHookErr, when set, is returned by DeleteProjectHook.
	DeleteHookErr error

	// NextPage is returned as PageInfo.NextPage by every list method.
	NextPage int

	// CallLog records every method invocation, in order.
	CallLog []Call
}

var _ Client = (*FakeClient)(nil)

func (f *FakeClient) record(method string, args ...any) {
	f.CallLog = append(f.CallLog, Call{Method: method, Args: args})
}

// GetAuthenticatedUser implements Client.
func (f *FakeClient) GetAuthenticatedUser(ctx context.Context, personalAccessToken string) (*User, error) {
	f.Calls++
	f.record("GetAuthenticatedUser", personalAccessToken)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.AuthenticatedUser, nil
}

// ListMemberProjects implements Client.
func (f *FakeClient) ListMemberProjects(ctx context.Context, personalAccessToken string, opts ListOptions) ([]Project, PageInfo, error) {
	f.record("ListMemberProjects", personalAccessToken, opts)
	if f.ProjectsErr != nil {
		return nil, PageInfo{}, f.ProjectsErr
	}
	return f.Projects, PageInfo{NextPage: f.NextPage}, nil
}

// GetProject implements Client.
func (f *FakeClient) GetProject(ctx context.Context, personalAccessToken string, projectID int64) (*Project, error) {
	f.record("GetProject", personalAccessToken, projectID)
	if f.ProjectErr != nil {
		return nil, f.ProjectErr
	}
	return f.Project, nil
}

// ListProjectMembers implements Client.
func (f *FakeClient) ListProjectMembers(ctx context.Context, personalAccessToken string, projectID int64, opts ListOptions) ([]User, PageInfo, error) {
	f.record("ListProjectMembers", personalAccessToken, projectID, opts)
	if f.MembersErr != nil {
		return nil, PageInfo{}, f.MembersErr
	}
	return f.Members, PageInfo{NextPage: f.NextPage}, nil
}

// ListProjectLabels implements Client.
func (f *FakeClient) ListProjectLabels(ctx context.Context, personalAccessToken string, projectID int64, opts ListOptions) ([]Label, PageInfo, error) {
	f.record("ListProjectLabels", personalAccessToken, projectID, opts)
	if f.LabelsErr != nil {
		return nil, PageInfo{}, f.LabelsErr
	}
	return f.Labels, PageInfo{NextPage: f.NextPage}, nil
}

// ListIssues implements Client.
func (f *FakeClient) ListIssues(ctx context.Context, personalAccessToken string, projectID int64, opts ListIssuesOptions) ([]Issue, PageInfo, error) {
	f.record("ListIssues", personalAccessToken, projectID, opts)
	if f.IssuesErr != nil {
		return nil, PageInfo{}, f.IssuesErr
	}
	if f.IssuesPages != nil {
		page := opts.Page
		if page < 1 {
			page = 1
		}
		idx := page - 1
		if idx >= len(f.IssuesPages) {
			return nil, PageInfo{}, nil
		}
		next := 0
		if idx+1 < len(f.IssuesPages) {
			next = page + 1
		}
		return f.IssuesPages[idx], PageInfo{NextPage: next}, nil
	}
	return f.Issues, PageInfo{NextPage: f.NextPage}, nil
}

// GetIssue implements Client.
func (f *FakeClient) GetIssue(ctx context.Context, personalAccessToken string, projectID, issueIID int64) (*Issue, error) {
	f.record("GetIssue", personalAccessToken, projectID, issueIID)
	if f.GetIssueErr != nil {
		return nil, f.GetIssueErr
	}
	return f.Issue, nil
}

// CreateIssue implements Client.
func (f *FakeClient) CreateIssue(ctx context.Context, personalAccessToken string, projectID int64, payload IssuePayload) (*Issue, error) {
	f.record("CreateIssue", personalAccessToken, projectID, payload)
	if f.CreateIssueErr != nil {
		return nil, f.CreateIssueErr
	}
	return f.Issue, nil
}

// UpdateIssue implements Client.
func (f *FakeClient) UpdateIssue(ctx context.Context, personalAccessToken string, projectID, issueIID int64, payload UpdateIssuePayload) (*Issue, error) {
	f.record("UpdateIssue", personalAccessToken, projectID, issueIID, payload)
	if f.UpdateIssueErr != nil {
		return nil, f.UpdateIssueErr
	}
	return f.Issue, nil
}

// CreateNote implements Client.
func (f *FakeClient) CreateNote(ctx context.Context, personalAccessToken string, projectID, issueIID int64, payload CreateNotePayload) (*Note, error) {
	f.record("CreateNote", personalAccessToken, projectID, issueIID, payload)
	if f.CreateNoteErr != nil {
		return nil, f.CreateNoteErr
	}
	if f.Note != nil {
		return f.Note, nil
	}
	f.nextNoteID++
	return &Note{ID: f.nextNoteID, Body: payload.Body}, nil
}

// ListProjectHooks implements Client.
func (f *FakeClient) ListProjectHooks(ctx context.Context, personalAccessToken string, projectID int64) ([]ProjectHook, error) {
	f.record("ListProjectHooks", personalAccessToken, projectID)
	if f.HooksErr != nil {
		return nil, f.HooksErr
	}
	return f.Hooks, nil
}

// CreateProjectHook implements Client.
func (f *FakeClient) CreateProjectHook(ctx context.Context, personalAccessToken string, projectID int64, hook ProjectHook) (*ProjectHook, error) {
	f.record("CreateProjectHook", personalAccessToken, projectID, hook)
	if f.CreateHookErr != nil {
		return nil, f.CreateHookErr
	}
	return f.Hook, nil
}

// UpdateProjectHook implements Client.
func (f *FakeClient) UpdateProjectHook(ctx context.Context, personalAccessToken string, projectID, hookID int64, hook ProjectHook) (*ProjectHook, error) {
	f.record("UpdateProjectHook", personalAccessToken, projectID, hookID, hook)
	if f.UpdateHookErr != nil {
		return nil, f.UpdateHookErr
	}
	return f.Hook, nil
}

// DeleteProjectHook implements Client.
func (f *FakeClient) DeleteProjectHook(ctx context.Context, personalAccessToken string, projectID, hookID int64) error {
	f.record("DeleteProjectHook", personalAccessToken, projectID, hookID)
	return f.DeleteHookErr
}
