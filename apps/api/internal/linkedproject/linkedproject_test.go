package linkedproject_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/linkedproject"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New([]byte("01234567890123456789012345678901"[:32]))
	require.NoError(t, err)
	return c
}

// fixture bundles a linkedproject Service backed by an in-memory querier
// with an owner and a project whose GitLab connection is already seeded, so
// each test can go straight to exercising Create/List/Update/Delete. Its
// GitLab calls all go to fake instead of a real GitLab CE instance.
type fixture struct {
	svc       *linkedproject.Service
	q         *dbtest.FakeQuerier
	cipher    *crypto.Cipher
	ownerID   uuid.UUID
	projectID uuid.UUID
}

// newFixture builds a fixture with webhook registration disabled
// (APP_PUBLIC_URL unset), for tests that don't exercise it.
func newFixture(t *testing.T, fake *gitlab.FakeClient) fixture {
	t.Helper()
	return newFixtureWithAppURL(t, fake, "")
}

// newFixtureWithAppURL is newFixture, but with APP_PUBLIC_URL set to
// appPublicURL, so Create/RegisterWebhook attempt webhook registration
// against fake.
func newFixtureWithAppURL(t *testing.T, fake *gitlab.FakeClient, appPublicURL string) fixture {
	t.Helper()
	q := dbtest.New()
	cipher := testCipher(t)
	encryptedToken, err := cipher.Encrypt("glpat-test-token")
	require.NoError(t, err)

	projects := project.NewService(q)
	gitlabConns := gitlabconn.NewService(q, projects, cipher, func(string) gitlab.Client { return fake })
	svc := linkedproject.NewService(q, dbtest.FakeTxRunner{Q: q}, projects, gitlabConns, cipher, appPublicURL)

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	q.SeedGitlabConnection(p.ID, encryptedToken)

	return fixture{svc: svc, q: q, cipher: cipher, ownerID: owner.ID, projectID: p.ID}
}

func TestInScope(t *testing.T) {
	tests := []struct {
		name        string
		issueLabels []string
		scope       string
		syncLabels  []string
		want        bool
	}{
		{"scope all always matches", []string{}, linkedproject.ScopeAll, nil, true},
		{"scope all matches even with unrelated labels", []string{"bug"}, linkedproject.ScopeAll, []string{"feature"}, true},
		{"scope labels matches on overlap", []string{"bug", "urgent"}, linkedproject.ScopeLabels, []string{"urgent"}, true},
		{"scope labels rejects no overlap", []string{"bug"}, linkedproject.ScopeLabels, []string{"urgent"}, false},
		{"scope labels rejects empty issue labels", []string{}, linkedproject.ScopeLabels, []string{"urgent"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linkedproject.InScope(tt.issueLabels, tt.scope, tt.syncLabels)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestService_Create_RejectsMissingLabelsForLabelsScope(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "demo"}})

	_, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{
		GitlabProjectID: 1,
		SyncScope:       linkedproject.ScopeLabels,
		SyncLabels:      nil,
	})
	assert.ErrorIs(t, err, linkedproject.ErrSyncLabelsRequired)
}

func TestService_Create_RejectsInvalidSyncScope(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "demo"}})

	_, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{
		GitlabProjectID: 1,
		SyncScope:       "bogus",
	})
	assert.ErrorIs(t, err, linkedproject.ErrInvalidSyncScope)
}

func TestService_Create_FetchesMetadataFromGitlabAndStoresIt(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{Project: &gitlab.Project{
		ID:                42,
		Name:              "demo",
		PathWithNamespace: "group/demo",
		WebURL:            "https://gitlab.example.com/group/demo",
	}})

	link, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{
		GitlabProjectID: 42,
		SyncScope:       linkedproject.ScopeAll,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), link.GitlabProjectID)
	assert.Equal(t, "group/demo", link.PathWithNamespace)
	assert.Equal(t, "https://gitlab.example.com/group/demo", link.WebURL)
	assert.True(t, link.IsDefault, "the first linked project must become the default")
}

func TestService_Create_RejectsDuplicateGitlabProjectInSameConnection(t *testing.T) {
	fake := &gitlab.FakeClient{Project: &gitlab.Project{ID: 42, Name: "demo"}}
	f := newFixture(t, fake)

	_, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 42, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)

	_, err = f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 42, SyncScope: linkedproject.ScopeAll})
	assert.ErrorIs(t, err, linkedproject.ErrAlreadyLinked)
}

func TestService_Create_OnlyFirstLinkIsDefault(t *testing.T) {
	fake := &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "one"}}
	f := newFixture(t, fake)

	first, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 1, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)
	assert.True(t, first.IsDefault)

	fake.Project = &gitlab.Project{ID: 2, Name: "two"}
	second, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 2, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)
	assert.False(t, second.IsDefault)
}

func TestService_Update_CanChangeSyncScopeAndPromoteDefault(t *testing.T) {
	fake := &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "one"}}
	f := newFixture(t, fake)

	first, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 1, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)
	fake.Project = &gitlab.Project{ID: 2, Name: "two"}
	second, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 2, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)

	updated, err := f.svc.Update(context.Background(), f.ownerID, second.ID, linkedproject.UpdateParams{
		SyncScope:  linkedproject.ScopeLabels,
		SyncLabels: []string{"bug"},
		SetDefault: true,
	})
	require.NoError(t, err)
	assert.Equal(t, linkedproject.ScopeLabels, updated.SyncScope)
	assert.Equal(t, []string{"bug"}, updated.SyncLabels)
	assert.True(t, updated.IsDefault)

	all, err := f.svc.List(context.Background(), f.ownerID, f.projectID)
	require.NoError(t, err)
	for _, l := range all {
		if l.ID == first.ID {
			assert.False(t, l.IsDefault, "promoting a new default must demote the old one")
		}
	}
}

func TestService_Update_RejectsMissingLabelsForLabelsScope(t *testing.T) {
	fake := &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "one"}}
	f := newFixture(t, fake)

	link, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 1, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)

	_, err = f.svc.Update(context.Background(), f.ownerID, link.ID, linkedproject.UpdateParams{SyncScope: linkedproject.ScopeLabels})
	assert.ErrorIs(t, err, linkedproject.ErrSyncLabelsRequired)
}

func TestService_Delete_KeepsOtherLinksAndPromotesOldestAsDefault(t *testing.T) {
	fake := &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "one"}}
	f := newFixture(t, fake)

	first, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 1, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)
	fake.Project = &gitlab.Project{ID: 2, Name: "two"}
	second, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 2, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)
	require.True(t, first.IsDefault)

	require.NoError(t, f.svc.Delete(context.Background(), f.ownerID, first.ID))

	remaining, err := f.svc.List(context.Background(), f.ownerID, f.projectID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, second.ID, remaining[0].ID)
	assert.True(t, remaining[0].IsDefault, "deleting the default link must promote the oldest remaining one")
}

func TestService_Delete_ReturnsNotFoundForForeignOrMissingLink(t *testing.T) {
	fake := &gitlab.FakeClient{Project: &gitlab.Project{ID: 1, Name: "one"}}
	f := newFixture(t, fake)
	other := f.q.SeedUser("hubot", "hubot@example.com").ID

	link, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{GitlabProjectID: 1, SyncScope: linkedproject.ScopeAll})
	require.NoError(t, err)

	err = f.svc.Delete(context.Background(), other, link.ID)
	assert.ErrorIs(t, err, linkedproject.ErrNotFound)

	err = f.svc.Delete(context.Background(), f.ownerID, uuid.New())
	assert.ErrorIs(t, err, linkedproject.ErrNotFound)
}

func TestService_ListAvailable_ReturnsGitlabMemberProjects(t *testing.T) {
	fake := &gitlab.FakeClient{Projects: []gitlab.Project{{ID: 1, Name: "demo"}}}
	f := newFixture(t, fake)

	projects, _, err := f.svc.ListAvailable(context.Background(), f.ownerID, f.projectID, linkedproject.AvailableProjectsParams{Search: "demo"})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "demo", projects[0].Name)

	require.NotEmpty(t, fake.CallLog)
	last := fake.CallLog[len(fake.CallLog)-1]
	assert.Equal(t, "ListMemberProjects", last.Method)
	opts, ok := last.Args[1].(gitlab.ListOptions)
	require.True(t, ok)
	assert.Equal(t, "demo", opts.Search)
}

func TestService_List_ReturnsNotFoundForForeignOrMissingProject(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{})
	other := f.q.SeedUser("hubot", "hubot@example.com").ID

	_, err := f.svc.List(context.Background(), other, f.projectID)
	assert.ErrorIs(t, err, linkedproject.ErrNotFound)

	_, err = f.svc.List(context.Background(), f.ownerID, uuid.New())
	assert.ErrorIs(t, err, linkedproject.ErrNotFound)
}

// TestService_Create_RegistersWebhook covers issue #18's three outcomes for
// webhook registration at link-creation time: it succeeds, GitLab rejects it
// for insufficient permission, or APP_PUBLIC_URL is unset and registration
// is skipped entirely — in every case Create itself still succeeds.
func TestService_Create_RegistersWebhook(t *testing.T) {
	tests := []struct {
		name             string
		appPublicURL     string
		hook             *gitlab.ProjectHook
		createHookErr    error
		wantStatus       string
		wantErrSubstring string
		wantHookCall     bool
	}{
		{
			name:         "registers successfully",
			appPublicURL: "https://flowlens.example.com",
			hook:         &gitlab.ProjectHook{ID: 501},
			wantStatus:   linkedproject.WebhookStatusRegistered,
			wantHookCall: true,
		},
		{
			name:             "insufficient gitlab permission is recorded but does not fail create",
			appPublicURL:     "https://flowlens.example.com",
			createHookErr:    &gitlab.APIError{StatusCode: 403, Body: "insufficient permissions"},
			wantStatus:       linkedproject.WebhookStatusFailed,
			wantErrSubstring: "Maintainer",
			wantHookCall:     true,
		},
		{
			name:         "app_public_url unset skips registration",
			appPublicURL: "",
			wantStatus:   linkedproject.WebhookStatusNotRegistered,
			wantHookCall: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &gitlab.FakeClient{
				Project:       &gitlab.Project{ID: 42, Name: "demo"},
				Hook:          tt.hook,
				CreateHookErr: tt.createHookErr,
			}
			f := newFixtureWithAppURL(t, fake, tt.appPublicURL)

			link, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{
				GitlabProjectID: 42,
				SyncScope:       linkedproject.ScopeAll,
			})
			require.NoError(t, err, "webhook registration outcome must never fail Create")
			assert.Equal(t, tt.wantStatus, link.WebhookStatus)
			if tt.wantErrSubstring != "" {
				assert.Contains(t, link.WebhookError, tt.wantErrSubstring)
			} else {
				assert.Empty(t, link.WebhookError)
			}
			if tt.wantStatus == linkedproject.WebhookStatusRegistered {
				assert.NotNil(t, link.WebhookRegisteredAt)
			}

			var sawCreateHook bool
			for _, call := range fake.CallLog {
				if call.Method == "CreateProjectHook" {
					sawCreateHook = true
				}
			}
			assert.Equal(t, tt.wantHookCall, sawCreateHook)
		})
	}
}

// TestService_Create_EncryptsWebhookSecretAndNeverExposesIt covers issue
// #18's acceptance criterion that the webhook secret is stored encrypted and
// never appears in the API-facing LinkedProject.
func TestService_Create_EncryptsWebhookSecretAndNeverExposesIt(t *testing.T) {
	fake := &gitlab.FakeClient{
		Project: &gitlab.Project{ID: 42, Name: "demo"},
		Hook:    &gitlab.ProjectHook{ID: 501},
	}
	f := newFixtureWithAppURL(t, fake, "https://flowlens.example.com")

	link, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{
		GitlabProjectID: 42,
		SyncScope:       linkedproject.ScopeAll,
	})
	require.NoError(t, err)
	require.Equal(t, linkedproject.WebhookStatusRegistered, link.WebhookStatus)

	body, err := json.Marshal(link)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(string(body)), "secret",
		"the API-facing LinkedProject must never carry the webhook secret")

	row, err := f.q.GetLinkedGitlabProjectForOwner(context.Background(), db.GetLinkedGitlabProjectForOwnerParams{
		ID:          link.ID,
		OwnerUserID: f.ownerID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, row.EncryptedWebhookSecret, "the secret must be persisted")

	secret, err := f.cipher.Decrypt(row.EncryptedWebhookSecret)
	require.NoError(t, err)
	assert.NotEmpty(t, secret)
	assert.NotEqual(t, secret, string(row.EncryptedWebhookSecret), "the stored secret must be ciphertext, not plaintext")
}

// TestService_RegisterWebhook_IsIdempotent covers issue #18's acceptance
// criterion that repairing a link's webhook twice never creates a duplicate
// hook: RegisterWebhook lists GitLab's existing hooks first and updates the
// one it finds instead of blindly creating another.
func TestService_RegisterWebhook_IsIdempotent(t *testing.T) {
	fake := &gitlab.FakeClient{
		Project: &gitlab.Project{ID: 42, Name: "demo"},
		Hook:    &gitlab.ProjectHook{ID: 501},
	}
	appPublicURL := "https://flowlens.example.com"
	f := newFixtureWithAppURL(t, fake, appPublicURL)

	created, err := f.svc.Create(context.Background(), f.ownerID, f.projectID, linkedproject.CreateParams{
		GitlabProjectID: 42,
		SyncScope:       linkedproject.ScopeAll,
	})
	require.NoError(t, err)
	require.Equal(t, linkedproject.WebhookStatusRegistered, created.WebhookStatus)

	// Simulate the hook now existing on GitLab, as ListProjectHooks would
	// report after the registration above.
	hookURL := appPublicURL + "/webhooks/gitlab/" + created.ID.String()
	fake.Hooks = []gitlab.ProjectHook{{ID: 501, URL: hookURL}}

	_, err = f.svc.RegisterWebhook(context.Background(), f.ownerID, created.ID)
	require.NoError(t, err)
	repaired, err := f.svc.RegisterWebhook(context.Background(), f.ownerID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, linkedproject.WebhookStatusRegistered, repaired.WebhookStatus)

	var createCalls, updateCalls int
	for _, call := range fake.CallLog {
		switch call.Method {
		case "CreateProjectHook":
			createCalls++
		case "UpdateProjectHook":
			updateCalls++
		}
	}
	assert.Equal(t, 1, createCalls, "repairing an already-registered webhook must never create a duplicate hook")
	assert.Equal(t, 2, updateCalls, "each repair call must rotate the secret via update")
}
