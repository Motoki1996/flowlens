// Package projectinvite manages project_invites rows: the credential that
// lets someone who has no FlowLens account at all join one project (issue
// #211).
//
// Until this existed, adding a member meant resolving a username or email
// against users (internal/projectmember), so the invitee had to have
// registered already — and the only way to register is POST /auth/signup,
// which docs/self-hosting.md tells operators to close with
// ALLOW_SIGNUP=false. A hardened instance therefore could not onboard
// anyone. An invite reopens that door for one person, once, instead of
// reopening registration for everyone.
//
// Invites are issued and stored the way internal/apitoken handles bearer
// tokens: the raw value is returned only at creation and the database keeps
// just its SHA-256 hash (auth.HashToken). Issuing, listing and revoking are
// owner-only, the same rule apitoken and projectmember already follow —
// who can reach a project at all is a project-management decision.
//
// There is deliberately no email delivery here. FlowLens has no mail
// transport and targets closed, often air-gapped networks, so the invite
// URL is handed over by a human.
package projectinvite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. ErrNotFound covers both "no such
// project/invite" and "belongs to someone else", never distinguishing them.
//
// ErrInviteInvalid is the single error every acceptance-path failure
// collapses into — unknown token, expired, and already accepted are not
// told apart, because the caller presenting a token is unauthenticated and
// must not be able to probe which invites ever existed.
var (
	ErrNotFound      = errors.New("projectinvite: not found")
	ErrForbidden     = errors.New("projectinvite: forbidden")
	ErrInvalidRole   = errors.New("projectinvite: role must be owner, member, or viewer")
	ErrInvalidExpiry = errors.New("projectinvite: expiry must be between 1 and 90 days")
	ErrInviteInvalid = errors.New("projectinvite: invite is invalid, expired or already used")
	ErrAlreadyMember = errors.New("projectinvite: already a member of this project")
)

// Expiry bounds for a new invite. An invite is a bearer credential handed
// over a chat window, so it is short-lived by default; the ceiling exists
// so "never expires" cannot be reached by typing a large number.
const (
	DefaultExpiryDays = 7
	MaxExpiryDays     = 90
)

// rawTokenPrefix marks every issued invite so it is identifiable by eye or
// by a secret-scanning tool, and distinguishable from an API token's
// "flt_". Like there, it is part of the value that gets hashed.
const rawTokenPrefix = "fli_"

// tokenPrefixDisplayChars is how many characters beyond rawTokenPrefix are
// kept in token_prefix, so an owner can tell two outstanding invites apart
// in a list without storing anything that helps reconstruct either.
const tokenPrefixDisplayChars = 8

// Invite status values, derived rather than stored: the row carries
// accepted_at and expires_at, and these are what a list UI shows.
const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusExpired  = "expired"
)

// Invite is the API-facing representation of a project_invites row. It
// never carries the token's hash or raw value — the raw value exists only
// in Service.Create's return, immediately after issuance.
type Invite struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"projectId"`
	Role        string     `json:"role"`
	TokenPrefix string     `json:"tokenPrefix"`
	Status      string     `json:"status"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	AcceptedAt  *time.Time `json:"acceptedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// Preview is what a holder of an invite token is told before deciding to
// accept: which project, and as what. It carries no member list and no
// project description — only what the acceptance screen has to render.
type Preview struct {
	ProjectID   uuid.UUID `json:"projectId"`
	ProjectName string    `json:"projectName"`
	Role        string    `json:"role"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// tokenPrefixFor returns the leading slice of a raw token to store in
// token_prefix.
func tokenPrefixFor(rawToken string) string {
	n := len(rawTokenPrefix) + tokenPrefixDisplayChars
	if n > len(rawToken) {
		n = len(rawToken)
	}
	return rawToken[:n]
}

// statusOf derives an invite's status from the two timestamps that decide
// it. Accepted wins over expired: an invite that was used and then aged out
// was still used.
func statusOf(row db.ProjectInvite, now time.Time) string {
	switch {
	case row.AcceptedAt.Valid:
		return StatusAccepted
	case !row.ExpiresAt.Time.After(now):
		return StatusExpired
	default:
		return StatusPending
	}
}

// fromRow maps a database row to the domain model.
func fromRow(row db.ProjectInvite) Invite {
	inv := Invite{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		Role:        row.Role,
		TokenPrefix: row.TokenPrefix,
		Status:      statusOf(row, time.Now()),
		ExpiresAt:   row.ExpiresAt.Time,
		CreatedAt:   row.CreatedAt.Time,
	}
	if row.AcceptedAt.Valid {
		acceptedAt := row.AcceptedAt.Time
		inv.AcceptedAt = &acceptedAt
	}
	return inv
}

// Service manages a project's invites.
type Service struct {
	q        db.Querier
	txRunner database.TxRunner
	projects *project.Service
}

// NewService constructs a projectinvite Service. txRunner is what makes
// acceptance atomic: spending the invite and creating the membership it
// grants must either both happen or neither.
func NewService(q db.Querier, txRunner database.TxRunner, projects *project.Service) *Service {
	return &Service{q: q, txRunner: txRunner, projects: projects}
}

// authorize requires callerID to hold project.RoleOwner on projectID.
// Issuing a credential that admits a new person to the project is a
// project-management action, the same owner-minimum internal/apitoken and
// internal/projectmember already apply.
func (s *Service) authorize(ctx context.Context, callerID, projectID uuid.UUID) error {
	err := s.projects.Authorize(ctx, callerID, projectID, project.RoleOwner)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return ErrForbidden
	default:
		return fmt.Errorf("projectinvite: authorize: %w", err)
	}
}

// normalizeRole validates raw against the project_members vocabulary. An
// empty role defaults to "member", matching the column's own default.
func normalizeRole(raw string) (string, error) {
	if raw == "" {
		return project.RoleMember.String(), nil
	}
	switch raw {
	case project.RoleOwner.String(), project.RoleMember.String(), project.RoleViewer.String():
		return raw, nil
	default:
		return "", ErrInvalidRole
	}
}

// Create issues an invite for projectID and returns both the record and the
// raw token — the only place the raw value is ever available, mirroring
// apitoken.Service.Create. expiresInDays of 0 means DefaultExpiryDays.
func (s *Service) Create(ctx context.Context, callerID, projectID uuid.UUID, role string, expiresInDays int) (Invite, string, error) {
	if err := s.authorize(ctx, callerID, projectID); err != nil {
		return Invite{}, "", err
	}

	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return Invite{}, "", err
	}
	if expiresInDays == 0 {
		expiresInDays = DefaultExpiryDays
	}
	if expiresInDays < 1 || expiresInDays > MaxExpiryDays {
		return Invite{}, "", ErrInvalidExpiry
	}

	body, err := auth.NewOpaqueToken()
	if err != nil {
		return Invite{}, "", fmt.Errorf("projectinvite: create: %w", err)
	}
	rawToken := rawTokenPrefix + body

	row, err := s.q.CreateProjectInvite(ctx, db.CreateProjectInviteParams{
		ProjectID:       projectID,
		TokenHash:       auth.HashToken(rawToken),
		TokenPrefix:     tokenPrefixFor(rawToken),
		Role:            normalizedRole,
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, expiresInDays), Valid: true},
		CreatedByUserID: uuidToPg(callerID),
	})
	if err != nil {
		return Invite{}, "", fmt.Errorf("projectinvite: create: %w", err)
	}
	return fromRow(row), rawToken, nil
}

// List returns every invite issued for projectID, newest first, including
// the accepted and expired ones — an owner auditing who was let in needs
// the spent ones, not just the outstanding.
func (s *Service) List(ctx context.Context, callerID, projectID uuid.UUID) ([]Invite, error) {
	if err := s.authorize(ctx, callerID, projectID); err != nil {
		return nil, err
	}

	rows, err := s.q.ListProjectInvitesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("projectinvite: list: %w", err)
	}
	out := make([]Invite, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Revoke deletes an invite. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted.
func (s *Service) Revoke(ctx context.Context, callerID, inviteID uuid.UUID) error {
	affected, err := s.q.DeleteProjectInviteForOwner(ctx, db.DeleteProjectInviteForOwnerParams{
		ID:     inviteID,
		UserID: callerID,
	})
	if err != nil {
		return fmt.Errorf("projectinvite: revoke: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Preview resolves a raw invite token to the project and role it grants,
// for the acceptance screen to render before anyone commits to anything.
// It is reachable unauthenticated — the token is the credential — and
// returns ErrInviteInvalid for anything that cannot be accepted, so a
// caller can never tell an expired invite from one that never existed.
func (s *Service) Preview(ctx context.Context, rawToken string) (Preview, error) {
	row, err := s.lookup(ctx, s.q, rawToken)
	if err != nil {
		return Preview{}, err
	}
	return previewFrom(row), nil
}

func previewFrom(row db.GetProjectInviteByTokenHashRow) Preview {
	return Preview{
		ProjectID:   row.ProjectInvite.ProjectID,
		ProjectName: row.ProjectName,
		Role:        row.ProjectInvite.Role,
		ExpiresAt:   row.ProjectInvite.ExpiresAt.Time,
	}
}

// lookup resolves a raw token to its invite row, collapsing every reason it
// cannot be accepted into ErrInviteInvalid.
func (s *Service) lookup(ctx context.Context, q db.Querier, rawToken string) (db.GetProjectInviteByTokenHashRow, error) {
	if rawToken == "" {
		return db.GetProjectInviteByTokenHashRow{}, ErrInviteInvalid
	}
	row, err := q.GetProjectInviteByTokenHash(ctx, auth.HashToken(rawToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.GetProjectInviteByTokenHashRow{}, ErrInviteInvalid
		}
		return db.GetProjectInviteByTokenHashRow{}, fmt.Errorf("projectinvite: lookup: %w", err)
	}
	if statusOf(row.ProjectInvite, time.Now()) != StatusPending {
		return db.GetProjectInviteByTokenHashRow{}, ErrInviteInvalid
	}
	return row, nil
}

// Accept spends an invite for userID and creates the membership it grants,
// atomically: the AcceptProjectInvite query's "accepted_at IS NULL" guard
// means two callers racing on one token cannot both win, and the shared
// transaction means a failed membership insert leaves the invite unspent.
//
// Someone who is already a member gets ErrAlreadyMember and the invite is
// *not* consumed — rolling back is what leaves it usable by the person it
// was actually meant for.
func (s *Service) Accept(ctx context.Context, rawToken string, userID uuid.UUID) (Preview, error) {
	var accepted Preview
	err := s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := s.lookup(ctx, q, rawToken)
		if err != nil {
			return err
		}

		// Checked before the invite is spent, not after: someone who is
		// already in the project must not burn a single-use invite that was
		// meant for someone else. Doing it as an explicit read rather than
		// leaning on the membership insert's unique violation to roll the
		// transaction back keeps that guarantee independent of rollback.
		if _, err := q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{
			ProjectID: row.ProjectInvite.ProjectID,
			UserID:    userID,
		}); err == nil {
			return ErrAlreadyMember
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("projectinvite: accept: member role: %w", err)
		}

		if _, err := q.AcceptProjectInvite(ctx, db.AcceptProjectInviteParams{
			ID:               row.ProjectInvite.ID,
			AcceptedByUserID: uuidToPg(userID),
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Another caller spent it between the lookup and here.
				return ErrInviteInvalid
			}
			return fmt.Errorf("projectinvite: accept: %w", err)
		}

		if _, err := q.AddProjectMember(ctx, db.AddProjectMemberParams{
			ProjectID: row.ProjectInvite.ProjectID,
			UserID:    userID,
			Role:      row.ProjectInvite.Role,
		}); err != nil {
			if isUniqueViolation(err) {
				return ErrAlreadyMember
			}
			return fmt.Errorf("projectinvite: accept: add member: %w", err)
		}

		accepted = previewFrom(row)
		return nil
	})
	if err != nil {
		return Preview{}, err
	}
	return accepted, nil
}

func uuidToPg(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
