// Package linkedproject contains the domain model and service that manage
// which GitLab CE projects a FlowLens project syncs issues with, each
// link's sync scope (docs/plans/issue-sync.md, "Sync scope"), and its
// FlowLens webhook registration (issue #18).
//
// The webhook secret is encrypted at rest (internal/crypto) and never
// crosses into an API response; LinkedProject exposes only a status
// ("registered" / "not_registered" / "failed") and, on failure, a reason.
package linkedproject

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectsync"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sync scope values for LinkedProject.SyncScope.
const (
	ScopeAll    = "all"
	ScopeLabels = "labels"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is also returned when a link exists but belongs to
// another user (via its connection's project), so callers never leak
// existence to non-owners.
var (
	ErrNotFound                 = errors.New("linkedproject: not found")
	ErrInvalidSyncScope         = errors.New(`linkedproject: sync_scope must be "all" or "labels"`)
	ErrSyncLabelsRequired       = errors.New(`linkedproject: sync_labels must have at least one label when sync_scope is "labels"`)
	ErrAlreadyLinked            = errors.New("linkedproject: gitlab project is already linked")
	ErrGitlabProjectUnavailable = errors.New("linkedproject: could not fetch the gitlab project")

	// ErrWebhookForbidden classifies a webhook registration failure caused by
	// the connection's personal access token belonging to a user below
	// Maintainer on the GitLab project (the minimum role GitLab CE requires
	// to manage project webhooks).
	ErrWebhookForbidden = errors.New("linkedproject: gitlab rejected the webhook registration; the personal access token's user needs at least the Maintainer role on this project")
	// ErrWebhookRegistrationFailed classifies any other webhook registration
	// failure (GitLab unreachable, unexpected response, ...).
	ErrWebhookRegistrationFailed = errors.New("linkedproject: could not register the webhook with gitlab")
)

// Webhook status values for LinkedProject.WebhookStatus.
const (
	WebhookStatusNotRegistered = "not_registered"
	WebhookStatusRegistered    = "registered"
	WebhookStatusFailed        = "failed"
)

// webhookSecretBytes is the size of the random webhook secret FlowLens
// generates for each link (docs/issue-sync.md's "32-byte random").
const webhookSecretBytes = 32

// LinkedProject is the API-facing representation of a GitLab project linked
// to sync issues with a FlowLens project.
type LinkedProject struct {
	ID                  uuid.UUID  `json:"id"`
	GitlabConnectionID  uuid.UUID  `json:"gitlabConnectionId"`
	GitlabProjectID     int64      `json:"gitlabProjectId"`
	PathWithNamespace   string     `json:"pathWithNamespace"`
	Name                string     `json:"name"`
	WebURL              string     `json:"webUrl"`
	SyncScope           string     `json:"syncScope"`
	SyncLabels          []string   `json:"syncLabels"`
	IsDefault           bool       `json:"isDefault"`
	InitialImportStatus string     `json:"initialImportStatus"`
	LastSyncedAt        *time.Time `json:"lastSyncedAt,omitempty"`
	// WebhookStatus is one of the WebhookStatus* constants. The webhook
	// secret itself is never exposed, only this derived status and, when it
	// is WebhookStatusFailed, WebhookError explaining why.
	WebhookStatus       string     `json:"webhookStatus"`
	WebhookRegisteredAt *time.Time `json:"webhookRegisteredAt,omitempty"`
	WebhookError        string     `json:"webhookError,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.LinkedGitlabProject) LinkedProject {
	lp := LinkedProject{
		ID:                  row.ID,
		GitlabConnectionID:  row.GitlabConnectionID,
		GitlabProjectID:     row.GitlabProjectID,
		PathWithNamespace:   row.PathWithNamespace,
		Name:                row.Name,
		WebURL:              row.WebUrl,
		SyncScope:           row.SyncScope,
		SyncLabels:          row.SyncLabels,
		IsDefault:           row.IsDefault,
		InitialImportStatus: row.InitialImportStatus,
		WebhookStatus:       WebhookStatusNotRegistered,
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}
	if row.LastSyncedAt.Valid {
		t := row.LastSyncedAt.Time
		lp.LastSyncedAt = &t
	}
	switch {
	case row.WebhookRegistrationError != "":
		lp.WebhookStatus = WebhookStatusFailed
		lp.WebhookError = row.WebhookRegistrationError
	case row.WebhookRegisteredAt.Valid:
		lp.WebhookStatus = WebhookStatusRegistered
		t := row.WebhookRegisteredAt.Time
		lp.WebhookRegisteredAt = &t
	}
	return lp
}

// InScope reports whether an issue carrying issueLabels falls inside a
// linked project's sync scope: every issue when scope is ScopeAll, or an
// issue carrying at least one of syncLabels when scope is ScopeLabels. It is
// pure, so both the initial import and the webhook apply pipeline
// (docs/plans/issue-sync.md) can share it without depending on Service.
func InScope(issueLabels []string, scope string, syncLabels []string) bool {
	if scope != ScopeLabels {
		return true
	}
	for _, want := range syncLabels {
		if slices.Contains(issueLabels, want) {
			return true
		}
	}
	return false
}

// validateSyncScope enforces that scope is one of ScopeAll/ScopeLabels and
// that ScopeLabels carries at least one label. It normalizes ScopeAll's
// labels to an empty slice, since scope "all" ignores whatever labels are
// passed.
func validateSyncScope(scope string, labels []string) ([]string, error) {
	switch scope {
	case ScopeAll:
		return []string{}, nil
	case ScopeLabels:
		if len(labels) == 0 {
			return nil, ErrSyncLabelsRequired
		}
		return labels, nil
	default:
		return nil, ErrInvalidSyncScope
	}
}

// AvailableProjectsParams narrows ListAvailable's GitLab project listing.
type AvailableProjectsParams struct {
	Search  string
	Page    int
	PerPage int
}

// CreateParams holds the fields accepted when linking a GitLab project.
type CreateParams struct {
	GitlabProjectID int64
	SyncScope       string
	SyncLabels      []string
}

// UpdateParams holds the fields accepted when updating a link. SetDefault,
// when true, makes this link the project's default destination for new
// tasks' issues; it is a one-way switch (there is no "unset", since exactly
// one link stays default as long as any link exists).
type UpdateParams struct {
	SyncScope  string
	SyncLabels []string
	SetDefault bool
}

// Service manages the GitLab projects linked to a project's GitLab
// connection, owned by a single user.
type Service struct {
	q            db.Querier
	txRunner     database.TxRunner
	projects     *project.Service
	gitlabConns  *gitlabconn.Service
	cipher       *crypto.Cipher
	appPublicURL string
}

// NewService constructs a linkedproject Service. cipher encrypts/decrypts
// each link's webhook secret at rest. appPublicURL is the URL GitLab must be
// able to reach to deliver webhooks (config.Config.AppPublicURL); when
// empty, webhook registration is skipped and every link stays
// WebhookStatusNotRegistered. txRunner backs Create's automatic
// project.import enqueue (projectsync.EnqueueImport, issue #25).
func NewService(q db.Querier, txRunner database.TxRunner, projects *project.Service, gitlabConns *gitlabconn.Service, cipher *crypto.Cipher, appPublicURL string) *Service {
	return &Service{q: q, txRunner: txRunner, projects: projects, gitlabConns: gitlabConns, cipher: cipher, appPublicURL: appPublicURL}
}

// mapConnErr maps a gitlabconn error (raised while dialing a project's
// GitLab connection) to a linkedproject sentinel.
func mapConnErr(err error) error {
	if errors.Is(err, gitlabconn.ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("linkedproject: %w", err)
}

// ListAvailable lists the GitLab projects the connection's token's user is
// a member of, for picking one to link. It calls the GitLab API directly;
// nothing here is read from or written to the database.
func (s *Service) ListAvailable(ctx context.Context, ownerID, projectID uuid.UUID, params AvailableProjectsParams) ([]gitlab.Project, gitlab.PageInfo, error) {
	client, token, _, err := s.gitlabConns.Dial(ctx, ownerID, projectID)
	if err != nil {
		return nil, gitlab.PageInfo{}, mapConnErr(err)
	}
	projects, page, err := client.ListMemberProjects(ctx, token, gitlab.ListOptions{
		Search:  params.Search,
		Page:    params.Page,
		PerPage: params.PerPage,
	})
	if err != nil {
		return nil, gitlab.PageInfo{}, fmt.Errorf("linkedproject: list available: %w", err)
	}
	return projects, page, nil
}

// ListMembers lists linkID's GitLab project's members, for a task's assignee
// picker. It returns ErrNotFound if linkID does not exist or belongs to
// another user.
func (s *Service) ListMembers(ctx context.Context, ownerID, linkID uuid.UUID, params AvailableProjectsParams) ([]gitlab.User, gitlab.PageInfo, error) {
	link, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{
		ID:          linkID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, gitlab.PageInfo{}, ErrNotFound
		}
		return nil, gitlab.PageInfo{}, fmt.Errorf("linkedproject: list members: %w", err)
	}

	client, token, err := s.gitlabConns.DialByConnectionID(ctx, ownerID, link.GitlabConnectionID)
	if err != nil {
		return nil, gitlab.PageInfo{}, mapConnErr(err)
	}
	members, page, err := client.ListProjectMembers(ctx, token, link.GitlabProjectID, gitlab.ListOptions{
		Search:  params.Search,
		Page:    params.Page,
		PerPage: params.PerPage,
	})
	if err != nil {
		return nil, gitlab.PageInfo{}, fmt.Errorf("linkedproject: list members: %w", err)
	}
	return members, page, nil
}

// ListLabels lists linkID's GitLab project's existing labels, for a task's
// label picker. It returns ErrNotFound if linkID does not exist or belongs
// to another user.
func (s *Service) ListLabels(ctx context.Context, ownerID, linkID uuid.UUID) ([]gitlab.Label, error) {
	link, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{
		ID:          linkID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("linkedproject: list labels: %w", err)
	}

	client, token, err := s.gitlabConns.DialByConnectionID(ctx, ownerID, link.GitlabConnectionID)
	if err != nil {
		return nil, mapConnErr(err)
	}
	labels, _, err := client.ListProjectLabels(ctx, token, link.GitlabProjectID, gitlab.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("linkedproject: list labels: %w", err)
	}
	return labels, nil
}

// Create links a GitLab project to projectID's connection. path_with_namespace,
// name and web_url are fetched from GitLab rather than trusted from the
// caller. A GitLab project already linked in the same connection is
// rejected with ErrAlreadyLinked. The first project linked to a connection
// becomes its default automatically (see UpdateParams.SetDefault to change
// it later).
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, params CreateParams) (LinkedProject, error) {
	labels, err := validateSyncScope(params.SyncScope, params.SyncLabels)
	if err != nil {
		return LinkedProject{}, err
	}

	client, token, connID, err := s.gitlabConns.Dial(ctx, ownerID, projectID)
	if err != nil {
		return LinkedProject{}, mapConnErr(err)
	}

	gp, err := client.GetProject(ctx, token, params.GitlabProjectID)
	if err != nil {
		return LinkedProject{}, fmt.Errorf("%w: %v", ErrGitlabProjectUnavailable, err)
	}

	row, err := s.q.CreateLinkedGitlabProject(ctx, db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: connID,
		GitlabProjectID:    gp.ID,
		PathWithNamespace:  gp.PathWithNamespace,
		Name:               gp.Name,
		WebUrl:             gp.WebURL,
		SyncScope:          params.SyncScope,
		SyncLabels:         labels,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return LinkedProject{}, ErrAlreadyLinked
		}
		return LinkedProject{}, fmt.Errorf("linkedproject: create: %w", err)
	}

	// Registering the webhook never fails Create: with APP_PUBLIC_URL unset
	// the link stays WebhookStatusNotRegistered, and a GitLab-side failure
	// (typically insufficient permission) is recorded as WebhookStatusFailed
	// instead — either way the link is usable via manual sync
	// (docs/plans/issue-sync.md).
	if s.appPublicURL != "" {
		row, err = s.establishWebhook(ctx, ownerID, client, token, row)
		if err != nil {
			return LinkedProject{}, err
		}
	}

	// Automatic initial import (issue #25): best-effort, like the webhook
	// registration above — a failure here is logged, not returned, since
	// the link is still fully usable via manual resync
	// (docs/plans/issue-sync.md).
	if _, err := projectsync.EnqueueImport(ctx, s.txRunner, row.ID, projectID); err != nil {
		slog.Error("linked gitlab project: enqueue initial import", "linked_gitlab_project_id", row.ID, "error", err)
	}

	return fromRow(row), nil
}

// Get returns one of projectID's linked GitLab projects. A link that does not
// exist, one belonging to another user, and one belonging to another project
// of the same user are all ErrNotFound — the indistinguishable-404 convention
// the rest of this Service follows, extended to the project boundary because
// the caller reaches a link through a project's URL. The project is resolved
// separately rather than being part of the link row: a linked project has no
// project_id column of its own, only a path through gitlab_connections (see
// the queries file).
func (s *Service) Get(ctx context.Context, ownerID, projectID, linkID uuid.UUID) (LinkedProject, error) {
	row, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{
		ID:          linkID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LinkedProject{}, ErrNotFound
		}
		return LinkedProject{}, fmt.Errorf("linkedproject: get: %w", err)
	}

	linkProjectID, err := s.ProjectID(ctx, linkID)
	if err != nil {
		return LinkedProject{}, fmt.Errorf("linkedproject: get: %w", err)
	}
	if linkProjectID != projectID {
		return LinkedProject{}, ErrNotFound
	}
	return fromRow(row), nil
}

// List returns every GitLab project linked to projectID's connection,
// oldest first. It returns ErrNotFound if projectID does not exist or
// belongs to another user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID) ([]LinkedProject, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("linkedproject: list: %w", err)
	}

	rows, err := s.q.ListLinkedGitlabProjectsForOwner(ctx, db.ListLinkedGitlabProjectsForOwnerParams{
		ID:          projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return nil, fmt.Errorf("linkedproject: list: %w", err)
	}
	out := make([]LinkedProject, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// ProjectID returns the project linkID belongs to (resolved through its
// GitLab connection), with no owner check — only requireTokenResourceProject
// (internal/http, issue #66) uses this, to compare against a bearer token's
// own project on GET /linked-gitlab-projects/{linkID}/sync-runs. Every other
// method on this Service already scopes by owner; this one exists because a
// token has no owner to join against in the first place.
func (s *Service) ProjectID(ctx context.Context, linkID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetLinkedGitlabProjectProjectID(ctx, linkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("linkedproject: project id: %w", err)
	}
	return projectID, nil
}

// Update changes linkID's sync scope and, if requested, promotes it to be
// its connection's default link. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is written.
func (s *Service) Update(ctx context.Context, ownerID, linkID uuid.UUID, params UpdateParams) (LinkedProject, error) {
	labels, err := validateSyncScope(params.SyncScope, params.SyncLabels)
	if err != nil {
		return LinkedProject{}, err
	}

	row, err := s.q.UpdateLinkedGitlabProjectSyncScopeForOwner(ctx, db.UpdateLinkedGitlabProjectSyncScopeForOwnerParams{
		ID:          linkID,
		OwnerUserID: ownerID,
		SyncScope:   params.SyncScope,
		SyncLabels:  labels,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LinkedProject{}, ErrNotFound
		}
		return LinkedProject{}, fmt.Errorf("linkedproject: update: %w", err)
	}

	if params.SetDefault && !row.IsDefault {
		if err := s.q.ClearDefaultLinkedGitlabProjectsForOwner(ctx, db.ClearDefaultLinkedGitlabProjectsForOwnerParams{
			ID:          linkID,
			OwnerUserID: ownerID,
		}); err != nil {
			return LinkedProject{}, fmt.Errorf("linkedproject: update: clear default: %w", err)
		}
		row, err = s.q.SetDefaultLinkedGitlabProjectForOwner(ctx, db.SetDefaultLinkedGitlabProjectForOwnerParams{
			ID:          linkID,
			OwnerUserID: ownerID,
		})
		if err != nil {
			return LinkedProject{}, fmt.Errorf("linkedproject: update: set default: %w", err)
		}
	}
	return fromRow(row), nil
}

// Delete unlinks linkID. This never touches GitLab-side issues, only
// FlowLens's own bookkeeping — any tasks that were mirroring issues through
// this link keep existing as local tasks (task_gitlab_links cascades away
// with the link; tasks does not). If linkID has a registered webhook, its
// removal from GitLab is attempted but never blocks the unlink: GitLab
// reporting it already gone counts as success, per issue #18, and so does
// any other failure (best-effort — this is app-side bookkeeping). If the
// deleted link was its connection's default, the oldest remaining link is
// promoted so a default always exists while any link does. Ownership is
// enforced by the query, so a non-owner gets ErrNotFound and nothing is
// deleted.
func (s *Service) Delete(ctx context.Context, ownerID, linkID uuid.UUID) error {
	deleted, err := s.q.DeleteLinkedGitlabProjectForOwner(ctx, db.DeleteLinkedGitlabProjectForOwnerParams{
		ID:          linkID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("linkedproject: delete: %w", err)
	}
	s.deleteWebhookBestEffort(ctx, ownerID, deleted)
	if deleted.IsDefault {
		if err := s.q.PromoteOldestLinkedGitlabProjectAsDefault(ctx, deleted.GitlabConnectionID); err != nil {
			return fmt.Errorf("linkedproject: delete: promote default: %w", err)
		}
	}
	return nil
}

// RegisterWebhook (repair) creates linkID's FlowLens webhook on GitLab if
// none exists yet, or rotates its secret if one does — see establishWebhook.
// Retrying it never creates a duplicate hook. Returns ErrNotFound if linkID
// does not exist or belongs to another user. When APP_PUBLIC_URL is unset
// the link is returned unchanged, still WebhookStatusNotRegistered.
func (s *Service) RegisterWebhook(ctx context.Context, ownerID, linkID uuid.UUID) (LinkedProject, error) {
	link, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{
		ID:          linkID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LinkedProject{}, ErrNotFound
		}
		return LinkedProject{}, fmt.Errorf("linkedproject: register webhook: %w", err)
	}
	if s.appPublicURL == "" {
		return fromRow(link), nil
	}

	client, token, err := s.gitlabConns.DialByConnectionID(ctx, ownerID, link.GitlabConnectionID)
	if err != nil {
		return LinkedProject{}, mapConnErr(err)
	}
	updated, err := s.establishWebhook(ctx, ownerID, client, token, link)
	if err != nil {
		return LinkedProject{}, err
	}
	return fromRow(updated), nil
}

// establishWebhook registers or rotates link's FlowLens webhook against
// GitLab: it lists the project's existing hooks and updates the one at
// FlowLens's webhook URL if found (rotating its secret), or creates a new
// one otherwise. This list-then-create-or-update path is shared by Create
// and RegisterWebhook, so a repair retry never creates a duplicate hook.
//
// A GitLab-side failure (most commonly insufficient Maintainer permission)
// is recorded on the row via recordWebhookError rather than returned, so the
// caller (Create or RegisterWebhook) still succeeds — the link stays usable
// via manual sync either way (docs/plans/issue-sync.md). Only a database or
// encryption failure is returned as an error.
func (s *Service) establishWebhook(ctx context.Context, ownerID uuid.UUID, client gitlab.Client, token string, link db.LinkedGitlabProject) (db.LinkedGitlabProject, error) {
	secret, err := generateWebhookSecret()
	if err != nil {
		return db.LinkedGitlabProject{}, fmt.Errorf("linkedproject: generate webhook secret: %w", err)
	}
	hookURL := webhookURL(s.appPublicURL, link.ID)
	payload := gitlab.ProjectHook{URL: hookURL, Token: secret, IssuesEvents: true}

	existingHooks, err := client.ListProjectHooks(ctx, token, link.GitlabProjectID)
	if err != nil {
		return s.recordWebhookError(ctx, ownerID, link, err)
	}

	var hook *gitlab.ProjectHook
	for _, h := range existingHooks {
		if h.URL == hookURL {
			hook, err = client.UpdateProjectHook(ctx, token, link.GitlabProjectID, h.ID, payload)
			break
		}
	}
	if hook == nil && err == nil {
		hook, err = client.CreateProjectHook(ctx, token, link.GitlabProjectID, payload)
	}
	if err != nil {
		return s.recordWebhookError(ctx, ownerID, link, err)
	}

	encryptedSecret, err := s.cipher.Encrypt(secret)
	if err != nil {
		return db.LinkedGitlabProject{}, fmt.Errorf("linkedproject: encrypt webhook secret: %w", err)
	}

	updated, err := s.q.SetLinkedGitlabProjectWebhookForOwner(ctx, db.SetLinkedGitlabProjectWebhookForOwnerParams{
		ID:                     link.ID,
		OwnerUserID:            ownerID,
		WebhookID:              pgtype.Int8{Int64: hook.ID, Valid: true},
		EncryptedWebhookSecret: encryptedSecret,
	})
	if err != nil {
		return db.LinkedGitlabProject{}, fmt.Errorf("linkedproject: save webhook: %w", err)
	}
	return updated, nil
}

// recordWebhookError persists a GitLab-side webhook registration failure on
// link without returning it as an error to the caller.
func (s *Service) recordWebhookError(ctx context.Context, ownerID uuid.UUID, link db.LinkedGitlabProject, cause error) (db.LinkedGitlabProject, error) {
	updated, err := s.q.SetLinkedGitlabProjectWebhookErrorForOwner(ctx, db.SetLinkedGitlabProjectWebhookErrorForOwnerParams{
		ID:                       link.ID,
		OwnerUserID:              ownerID,
		WebhookRegistrationError: classifyWebhookError(cause).Error(),
	})
	if err != nil {
		return db.LinkedGitlabProject{}, fmt.Errorf("linkedproject: record webhook error: %w", err)
	}
	return updated, nil
}

// deleteWebhookBestEffort removes link's webhook from GitLab if one was ever
// registered. Every failure — dialing the connection, GitLab reporting the
// hook already gone, or anything else — is swallowed: this is best-effort
// GitLab-side cleanup and must never block unlinking (Delete's own doc
// comment).
func (s *Service) deleteWebhookBestEffort(ctx context.Context, ownerID uuid.UUID, link db.LinkedGitlabProject) {
	if !link.WebhookID.Valid {
		return
	}
	client, token, err := s.gitlabConns.DialByConnectionID(ctx, ownerID, link.GitlabConnectionID)
	if err != nil {
		return
	}
	_ = client.DeleteProjectHook(ctx, token, link.GitlabProjectID, link.WebhookID.Int64)
}

// classifyWebhookError maps a GitLab API failure encountered while
// registering a webhook to a sentinel that distinguishes "the token's user
// lacks Maintainer access" from any other failure, so the reason stored on
// the row (and surfaced via LinkedProject.WebhookError) is meaningful.
func classifyWebhookError(err error) error {
	var apiErr *gitlab.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		return ErrWebhookForbidden
	}
	return fmt.Errorf("%w: %v", ErrWebhookRegistrationFailed, err)
}

// generateWebhookSecret returns a fresh random secret, hex-encoded, used as
// the GitLab webhook token that authenticates inbound deliveries
// (docs/plans/issue-sync.md, "Inbound").
func generateWebhookSecret() (string, error) {
	raw := make([]byte, webhookSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// webhookURL is the exact URL FlowLens registers as linkID's GitLab
// webhook, matching the inbound route in docs/plans/issue-sync.md.
func webhookURL(appPublicURL string, linkID uuid.UUID) string {
	return strings.TrimRight(appPublicURL, "/") + "/webhooks/gitlab/" + linkID.String()
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation, which for linked_gitlab_projects can only be
// (gitlab_connection_id, gitlab_project_id).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
