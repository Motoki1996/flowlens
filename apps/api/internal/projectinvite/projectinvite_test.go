package projectinvite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectinvite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *projectinvite.Service {
	return projectinvite.NewService(q, dbtest.FakeTxRunner{Q: q}, project.NewService(q))
}

func TestService_Create_ReturnsARawTokenTheDatabaseNeverStores(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	invite, rawToken, err := svc.Create(context.Background(), owner.ID, p.ID, "member", 0)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(rawToken, "fli_"), "an invite must be identifiable by its prefix, got %q", rawToken)
	assert.Equal(t, "member", invite.Role)
	assert.Equal(t, projectinvite.StatusPending, invite.Status)
	assert.True(t, strings.HasPrefix(rawToken, invite.TokenPrefix))

	// Only the hash is stored: the raw value must not be recoverable from
	// any row, which is why Create is the one place it is ever returned.
	stored, err := q.ListProjectInvitesByProject(context.Background(), p.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.NotEqual(t, rawToken, stored[0].TokenHash)
	assert.Equal(t, auth.HashToken(rawToken), stored[0].TokenHash)
}

func TestService_Create_DefaultsRoleAndExpiry(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	invite, _, err := svc.Create(context.Background(), owner.ID, p.ID, "", 0)
	require.NoError(t, err)

	assert.Equal(t, "member", invite.Role, "an unspecified role is the least privileged useful one")
	want := time.Now().AddDate(0, 0, projectinvite.DefaultExpiryDays)
	assert.WithinDuration(t, want, invite.ExpiresAt, time.Minute)
}

func TestService_Create_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name          string
		role          string
		expiresInDays int
		wantErr       error
	}{
		{"unknown role", "admin", 0, projectinvite.ErrInvalidRole},
		{"negative expiry", "member", -1, projectinvite.ErrInvalidExpiry},
		{"expiry beyond the ceiling", "member", projectinvite.MaxExpiryDays + 1, projectinvite.ErrInvalidExpiry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com")
			p := q.SeedProject(owner.ID, "Alpha")

			_, _, err := svc.Create(context.Background(), owner.ID, p.ID, tt.role, tt.expiresInDays)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestService_Create_IsOwnerOnly pins that issuing an invite is a
// project-management action: a member or viewer cannot let someone new in,
// and a stranger cannot tell the project exists.
func TestService_Create_IsOwnerOnly(t *testing.T) {
	tests := []struct {
		name    string
		role    string // "" seeds no membership at all
		wantErr error
	}{
		{"member", "member", projectinvite.ErrForbidden},
		{"viewer", "viewer", projectinvite.ErrForbidden},
		{"stranger", "", projectinvite.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com")
			p := q.SeedProject(owner.ID, "Alpha")
			caller := q.SeedUser("caller", "caller@example.com")
			if tt.role != "" {
				q.SeedProjectMember(p.ID, caller.ID, tt.role)
			}

			_, _, err := svc.Create(context.Background(), caller.ID, p.ID, "member", 0)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestService_Preview_NamesTheProjectAndRole(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, rawToken, err := svc.Create(context.Background(), owner.ID, p.ID, "viewer", 0)
	require.NoError(t, err)

	preview, err := svc.Preview(context.Background(), rawToken)
	require.NoError(t, err)
	assert.Equal(t, p.ID, preview.ProjectID)
	assert.Equal(t, "Alpha", preview.ProjectName)
	assert.Equal(t, "viewer", preview.Role)
}

// TestService_Preview_CollapsesEveryFailure pins that an unauthenticated
// caller cannot tell an expired invite from one that never existed — the
// three cases share one error on purpose.
func TestService_Preview_CollapsesEveryFailure(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	ctx := context.Background()

	expiredToken := "fli_expired"
	q.SeedProjectInvite(p.ID, auth.HashToken(expiredToken), "member", time.Now().Add(-time.Hour))

	spentToken := ""
	if _, raw, err := svc.Create(ctx, owner.ID, p.ID, "member", 0); err == nil {
		spentToken = raw
		joiner := q.SeedUser("joiner", "joiner@example.com")
		_, err = svc.Accept(ctx, raw, joiner.ID)
		require.NoError(t, err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"unknown token", "fli_never-existed"},
		{"expired invite", expiredToken},
		{"already accepted invite", spentToken},
		{"empty token", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Preview(ctx, tt.token)
			assert.ErrorIs(t, err, projectinvite.ErrInviteInvalid)
		})
	}
}

func TestService_Accept_CreatesTheMembershipAndSpendsTheInvite(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	joiner := q.SeedUser("joiner", "joiner@example.com")
	ctx := context.Background()

	_, rawToken, err := svc.Create(ctx, owner.ID, p.ID, "viewer", 0)
	require.NoError(t, err)

	accepted, err := svc.Accept(ctx, rawToken, joiner.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, accepted.ProjectID)

	// The membership is what the invite was for, at the role it named.
	projects := project.NewService(q)
	assert.NoError(t, projects.Authorize(ctx, joiner.ID, p.ID, project.RoleViewer))
	assert.ErrorIs(t, projects.Authorize(ctx, joiner.ID, p.ID, project.RoleMember), project.ErrForbidden,
		"the invite granted viewer, so it must not confer member")

	// Single-use: the same link cannot admit a second person.
	other := q.SeedUser("other", "other@example.com")
	_, err = svc.Accept(ctx, rawToken, other.ID)
	assert.ErrorIs(t, err, projectinvite.ErrInviteInvalid)

	invites, err := svc.List(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, invites, 1)
	assert.Equal(t, projectinvite.StatusAccepted, invites[0].Status)
}

// TestService_Accept_ExistingMemberLeavesTheInviteUnspent pins the rollback:
// handing an invite to someone who is already in the project must not burn
// the invite that was meant for someone else.
func TestService_Accept_ExistingMemberLeavesTheInviteUnspent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	ctx := context.Background()

	_, rawToken, err := svc.Create(ctx, owner.ID, p.ID, "member", 0)
	require.NoError(t, err)

	_, err = svc.Accept(ctx, rawToken, owner.ID)
	assert.ErrorIs(t, err, projectinvite.ErrAlreadyMember)

	// Still usable by the person it was actually for.
	joiner := q.SeedUser("joiner", "joiner@example.com")
	_, err = svc.Accept(ctx, rawToken, joiner.ID)
	assert.NoError(t, err)
}

func TestService_Accept_RejectsAnExpiredInvite(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	joiner := q.SeedUser("joiner", "joiner@example.com")

	rawToken := "fli_expired"
	q.SeedProjectInvite(p.ID, auth.HashToken(rawToken), "member", time.Now().Add(-time.Minute))

	_, err := svc.Accept(context.Background(), rawToken, joiner.ID)
	assert.ErrorIs(t, err, projectinvite.ErrInviteInvalid)
	assert.ErrorIs(t, project.NewService(q).Authorize(context.Background(), joiner.ID, p.ID, project.RoleViewer), project.ErrNotFound)
}

func TestService_List_ReportsStatusPerInvite(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	ctx := context.Background()

	_, _, err := svc.Create(ctx, owner.ID, p.ID, "member", 0)
	require.NoError(t, err)
	q.SeedProjectInvite(p.ID, auth.HashToken("fli_expired"), "member", time.Now().Add(-time.Hour))

	invites, err := svc.List(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, invites, 2)

	statuses := map[string]bool{}
	for _, inv := range invites {
		statuses[inv.Status] = true
	}
	assert.True(t, statuses[projectinvite.StatusPending])
	assert.True(t, statuses[projectinvite.StatusExpired], "an owner auditing invites needs the expired ones too")
}

func TestService_Revoke_IsOwnerOnlyAndMakesTheLinkDead(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	member := q.SeedUser("member", "member@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")
	ctx := context.Background()

	invite, rawToken, err := svc.Create(ctx, owner.ID, p.ID, "member", 0)
	require.NoError(t, err)

	assert.ErrorIs(t, svc.Revoke(ctx, member.ID, invite.ID), projectinvite.ErrNotFound,
		"a non-owner must not be able to revoke, nor learn the invite exists")

	require.NoError(t, svc.Revoke(ctx, owner.ID, invite.ID))
	_, err = svc.Preview(ctx, rawToken)
	assert.ErrorIs(t, err, projectinvite.ErrInviteInvalid)
}

func TestService_Revoke_UnknownInvite(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")

	assert.ErrorIs(t, svc.Revoke(context.Background(), owner.ID, uuid.New()), projectinvite.ErrNotFound)
}
