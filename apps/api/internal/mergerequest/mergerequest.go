// Package mergerequest is the read-only domain layer over merge_requests
// (issue #112, building on #111's mrsync). Unlike internal/task, it has no
// Create/Update/Close: FlowLens never writes a merge request back to GitLab
// (ADR-0011), so this package only lists and gets what mrsync has already
// imported.
package mergerequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. ErrNotFound also covers a merge
// request that exists but belongs to a project the caller isn't a member
// of, so callers never leak existence to non-members.
var (
	ErrNotFound  = errors.New("mergerequest: not found")
	ErrForbidden = errors.New("mergerequest: forbidden")
)

// Sort values ListFilter.Sort accepts. The empty string ("") sorts by
// gitlab_created_at DESC, the default "newest first" order; SortUpdated
// switches to gitlab_updated_at DESC.
const SortUpdated = "updated"

// Paging bounds for List. A repository that has been synced for a year or
// two holds thousands of merged merge requests, so the collection view is
// paged rather than returning all of them; these mirror
// webhookevent.DefaultPerPage/MaxPerPage.
const (
	DefaultPerPage = 30
	MaxPerPage     = 100
)

// MergeRequest is the domain model returned by List/Get.
type MergeRequest struct {
	ID                   uuid.UUID  `json:"id"`
	RepositoryID         uuid.UUID  `json:"repositoryId"`
	GitlabMergeRequestID int64      `json:"gitlabMergeRequestId"`
	Number               int32      `json:"number"`
	Title                string     `json:"title"`
	State                string     `json:"state"`
	IsDraft              bool       `json:"isDraft"`
	AuthorGitlabUsername string     `json:"authorGitlabUsername"`
	AuthorAvatarUrl      string     `json:"authorAvatarUrl"`
	BaseBranch           string     `json:"baseBranch"`
	HeadBranch           string     `json:"headBranch"`
	Additions            int32      `json:"additions"`
	Deletions            int32      `json:"deletions"`
	ChangedFiles         int32      `json:"changedFiles"`
	GitlabCreatedAt      *time.Time `json:"gitlabCreatedAt"`
	GitlabUpdatedAt      *time.Time `json:"gitlabUpdatedAt"`
	MergedAt             *time.Time `json:"mergedAt"`
	ClosedAt             *time.Time `json:"closedAt"`
	HtmlUrl              string     `json:"htmlUrl"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	FirstReviewedAt      *time.Time `json:"firstReviewedAt"`
	PipelineStatus       string     `json:"pipelineStatus"`
	PipelineID           *int64     `json:"pipelineId"`
	PipelineUpdatedAt    *time.Time `json:"pipelineUpdatedAt"`
	TaskID               *uuid.UUID `json:"taskId"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.MergeRequest) MergeRequest {
	return MergeRequest{
		ID:                   row.ID,
		RepositoryID:         row.RepositoryID,
		GitlabMergeRequestID: row.GitlabMergeRequestID,
		Number:               row.Number,
		Title:                row.Title,
		State:                row.State,
		IsDraft:              row.IsDraft,
		AuthorGitlabUsername: row.AuthorGitlabUsername,
		AuthorAvatarUrl:      row.AuthorAvatarUrl,
		BaseBranch:           row.BaseBranch,
		HeadBranch:           row.HeadBranch,
		Additions:            row.Additions,
		Deletions:            row.Deletions,
		ChangedFiles:         row.ChangedFiles,
		GitlabCreatedAt:      timePtr(row.GitlabCreatedAt),
		GitlabUpdatedAt:      timePtr(row.GitlabUpdatedAt),
		MergedAt:             timePtr(row.MergedAt),
		ClosedAt:             timePtr(row.ClosedAt),
		HtmlUrl:              row.HtmlUrl,
		CreatedAt:            row.CreatedAt.Time,
		UpdatedAt:            row.UpdatedAt.Time,
		FirstReviewedAt:      timePtr(row.FirstReviewedAt),
		PipelineStatus:       row.PipelineStatus,
		PipelineID:           int64Ptr(row.PipelineID),
		PipelineUpdatedAt:    timePtr(row.PipelineUpdatedAt),
		TaskID:               uuidPtr(row.TaskID),
	}
}

func uuidPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func int64Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func toUUID(v *uuid.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *v, Valid: true}
}

func toTimestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

// Service is the merge-request domain service. It has no txRunner: every
// method is a read, and there is nothing to enqueue.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a mergerequest Service. projects verifies project
// membership before any read.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

// authorize requires ownerID to hold at least min on projectID, mapping
// project.ErrNotFound/ErrForbidden to this package's own sentinels.
func (s *Service) authorize(ctx context.Context, ownerID, projectID uuid.UUID, min project.Role) error {
	err := s.projects.Authorize(ctx, ownerID, projectID, min)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return ErrForbidden
	default:
		return fmt.Errorf("mergerequest: authorize: %w", err)
	}
}

// ListFilter narrows List to a subset of a project's merge requests. The
// zero value means "no filter": every merge request synced for the project,
// newest (by gitlab_created_at) first.
type ListFilter struct {
	State  string     // one of the GitLab MR states ("opened", "merged", "closed"), or "" (no filter)
	Author string     // author_gitlab_username, or "" (no filter)
	TaskID *uuid.UUID // non-nil: only the merge request(s) linked to this task
	Since  *time.Time // non-nil: only merge requests GitLab created on or after this time
	Until  *time.Time // non-nil: only merge requests GitLab created on or before this time
	// Sort is "" (gitlab_created_at DESC, newest first) or SortUpdated
	// (gitlab_updated_at DESC). Both keep created_at DESC as a tiebreak for
	// merge requests with no GitLab timestamp yet.
	Sort string
	// Page is the 1-based page number; anything below 1 means the first
	// page. PerPage caps the page size: non-positive defaults to
	// DefaultPerPage, and anything above MaxPerPage is clamped to it.
	Page    int
	PerPage int
}

// Page is one page of List's results, in the order ListFilter.Sort asked
// for. NextPage is 0 when no further page follows, the same shape
// webhookevent.EventsPage uses. TotalCount is how many merge requests match
// the filter across every page — it is not derivable from a single page, and
// both the collection view's header and the project sidebar's badge need it.
type Page struct {
	MergeRequests []MergeRequest
	NextPage      int
	TotalCount    int64
}

// List returns one page of projectID's merge requests matching filter. It
// returns ErrNotFound if projectID does not exist or the caller isn't a
// member.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID, filter ListFilter) (Page, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return Page{}, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}

	// Fetch one extra row to tell whether another page follows, the same
	// "peek" pattern internal/webhookevent uses.
	rows, err := s.q.ListMergeRequestsByProject(ctx, db.ListMergeRequestsByProjectParams{
		ProjectID:     projectID,
		OwnerUserID:   ownerID,
		State:         filter.State,
		Author:        filter.Author,
		TaskID:        toUUID(filter.TaskID),
		Since:         toTimestamptz(filter.Since),
		Until:         toTimestamptz(filter.Until),
		SortByUpdated: filter.Sort == SortUpdated,
		LimitCount:    int32(perPage + 1),
		OffsetCount:   int32((page - 1) * perPage),
	})
	if err != nil {
		return Page{}, fmt.Errorf("mergerequest: list: %w", err)
	}

	nextPage := 0
	if len(rows) > perPage {
		rows = rows[:perPage]
		nextPage = page + 1
	}

	total, err := s.q.CountMergeRequestsByProject(ctx, db.CountMergeRequestsByProjectParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
		State:       filter.State,
		Author:      filter.Author,
		TaskID:      toUUID(filter.TaskID),
		Since:       toTimestamptz(filter.Since),
		Until:       toTimestamptz(filter.Until),
	})
	if err != nil {
		return Page{}, fmt.Errorf("mergerequest: count: %w", err)
	}

	out := make([]MergeRequest, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return Page{MergeRequests: out, NextPage: nextPage, TotalCount: total}, nil
}

// Get returns a single merge request, scoped to a project the caller is a
// member of. It returns ErrNotFound for both "doesn't exist" and "not
// visible to this caller".
func (s *Service) Get(ctx context.Context, ownerID, mergeRequestID uuid.UUID) (MergeRequest, error) {
	row, err := s.q.GetMergeRequestForOwner(ctx, db.GetMergeRequestForOwnerParams{ID: mergeRequestID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MergeRequest{}, ErrNotFound
		}
		return MergeRequest{}, fmt.Errorf("mergerequest: get: %w", err)
	}
	return fromRow(row), nil
}

// ProjectID returns the project mergeRequestID belongs to, with no
// membership check — only requireTokenResourceProject (internal/http)
// uses this, to compare against a bearer token's own project.
func (s *Service) ProjectID(ctx context.Context, mergeRequestID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetMergeRequestProjectID(ctx, mergeRequestID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("mergerequest: project id: %w", err)
	}
	return projectID, nil
}
