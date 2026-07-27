// Package linkedproject contains the domain model and service that manage
// which GitLab CE projects a FlowLens project syncs issues with, and each
// link's sync scope (docs/plans/issue-sync.md, "Sync scope").
//
// A link never carries webhook state: registering and unregistering the
// FlowLens webhook on the GitLab side is a separate concern (issue-sync
// phase 3+) built on top of this package once it lands.
package linkedproject

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
)

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
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}
	if row.LastSyncedAt.Valid {
		t := row.LastSyncedAt.Time
		lp.LastSyncedAt = &t
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
	q           db.Querier
	projects    *project.Service
	gitlabConns *gitlabconn.Service
}

// NewService constructs a linkedproject Service.
func NewService(q db.Querier, projects *project.Service, gitlabConns *gitlabconn.Service) *Service {
	return &Service{q: q, projects: projects, gitlabConns: gitlabConns}
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
// with the link; tasks does not). If the deleted link was its connection's
// default, the oldest remaining link is promoted so a default always exists
// while any link does. Ownership is enforced by the query, so a non-owner
// gets ErrNotFound and nothing is deleted.
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
	if deleted.IsDefault {
		if err := s.q.PromoteOldestLinkedGitlabProjectAsDefault(ctx, deleted.GitlabConnectionID); err != nil {
			return fmt.Errorf("linkedproject: delete: promote default: %w", err)
		}
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation, which for linked_gitlab_projects can only be
// (gitlab_connection_id, gitlab_project_id).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
