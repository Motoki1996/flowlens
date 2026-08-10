package apitoken_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *apitoken.Service {
	return apitoken.NewService(q, project.NewService(q))
}

func TestService_Create_ValidatesName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  CI bot  ", "CI bot", nil},
		{"rejects empty after trim", "   ", "", apitoken.ErrInvalidName},
		{"rejects too long", strings.Repeat("a", 101), "", apitoken.ErrInvalidName},
		{"accepts max length", strings.Repeat("a", 100), strings.Repeat("a", 100), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			token, _, err := svc.Create(context.Background(), owner, p.ID, tt.input, []string{apitoken.ScopeRead}, nil)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, token.Name)
		})
	}
}

func TestService_Create_ValidatesScopes(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr error
	}{
		{"rejects nil", nil, nil, apitoken.ErrInvalidScopes},
		{"rejects empty", []string{}, nil, apitoken.ErrInvalidScopes},
		{"rejects unknown value", []string{"admin"}, nil, apitoken.ErrInvalidScopes},
		{"rejects a valid value mixed with an unknown one", []string{"read", "admin"}, nil, apitoken.ErrInvalidScopes},
		{"accepts read alone", []string{"read"}, []string{"read"}, nil},
		{"normalizes a lone write to read+write", []string{"write"}, []string{"read", "write"}, nil},
		{"accepts read and write together", []string{"read", "write"}, []string{"read", "write"}, nil},
		{"collapses duplicates", []string{"read", "read"}, []string{"read"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			token, _, err := svc.Create(context.Background(), owner, p.ID, "CI bot", tt.input, nil)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, token.Scopes)
		})
	}
}

func TestService_Create_ReturnsNotFoundForForeignOrMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, _, err := svc.Create(context.Background(), other, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	assert.ErrorIs(t, err, apitoken.ErrNotFound)

	_, _, err = svc.Create(context.Background(), owner, uuid.New(), "CI bot", []string{apitoken.ScopeRead}, nil)
	assert.ErrorIs(t, err, apitoken.ErrNotFound)
}

func TestService_Create_ReturnsRawTokenOnlyOnce(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	token, raw, err := svc.Create(context.Background(), owner, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, raw)
	assert.True(t, strings.HasPrefix(raw, "flt_"), "raw token must carry the flt_ prefix")
	assert.Equal(t, p.ID, token.ProjectID)
	assert.Nil(t, token.ExpiresAt)
	assert.Nil(t, token.LastUsedAt)

	// The raw token must authenticate to the same project it was issued for.
	auth, err := svc.Authenticate(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, p.ID, auth.ProjectID)
}

func TestService_List_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.List(context.Background(), other, p.ID)
	assert.ErrorIs(t, err, apitoken.ErrNotFound)
}

func TestService_List_OrdersNewestFirst(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	first, _, err := svc.Create(ctx, owner, p.ID, "First", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)
	second, _, err := svc.Create(ctx, owner, p.ID, "Second", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	tokens, err := svc.List(ctx, owner, p.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	assert.Equal(t, second.ID, tokens[0].ID)
	assert.Equal(t, first.ID, tokens[1].ID)
}

func TestService_Delete_ReturnsNotFoundForForeignOrMissingToken(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	token, _, err := svc.Create(context.Background(), owner, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	assert.ErrorIs(t, svc.Delete(context.Background(), other, token.ID), apitoken.ErrNotFound)
	assert.ErrorIs(t, svc.Delete(context.Background(), owner, uuid.New()), apitoken.ErrNotFound)
}

func TestService_Delete_RevokesToken(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	token, raw, err := svc.Create(context.Background(), owner, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	require.NoError(t, svc.Delete(context.Background(), owner, token.ID))

	_, err = svc.Authenticate(context.Background(), raw)
	assert.ErrorIs(t, err, apitoken.ErrTokenNotFound)
}

func TestService_Authenticate(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	_, expiredRaw, err := svc.Create(ctx, owner, p.ID, "Expired", []string{apitoken.ScopeRead}, &past)
	require.NoError(t, err)
	activeToken, activeRaw, err := svc.Create(ctx, owner, p.ID, "Active", []string{apitoken.ScopeRead}, &future)
	require.NoError(t, err)

	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"empty token", "", apitoken.ErrTokenNotFound},
		{"unknown token", "bogus-token", apitoken.ErrTokenNotFound},
		{"expired token", expiredRaw, apitoken.ErrTokenNotFound},
		{"active token", activeRaw, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := svc.Authenticate(ctx, tt.raw)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, activeToken.ProjectID, auth.ProjectID)
		})
	}
}

func TestService_Authenticate_ReturnsOwnerAndScopes(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	_, raw, err := svc.Create(ctx, owner, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	auth, err := svc.Authenticate(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, p.ID, auth.ProjectID)
	assert.Equal(t, owner, auth.OwnerUserID)
	assert.Equal(t, []string{apitoken.ScopeRead, apitoken.ScopeWrite}, auth.Scopes)
	assert.True(t, auth.HasScope(apitoken.ScopeRead))
	assert.True(t, auth.HasScope(apitoken.ScopeWrite))
}

// TestService_Authenticate_ResolvesOwnerWithMultipleProjectMembers covers
// issue #99's point about ADR-0009: a token still authenticates correctly
// (resolving to the project's single designated owner_user_id) once other
// users have been added to the project as member/viewer, and that resolved
// owner's role-based checks all pass as owner — a bearer request "acts as"
// the project's owner regardless of how many other members exist.
func TestService_Authenticate_ResolvesOwnerWithMultipleProjectMembers(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	projects := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	member := q.SeedUser("member", "member@example.com").ID
	viewer := q.SeedUser("viewer", "viewer@example.com").ID
	q.SeedProjectMember(p.ID, member, "member")
	q.SeedProjectMember(p.ID, viewer, "viewer")

	_, raw, err := svc.Create(ctx, owner, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	auth, err := svc.Authenticate(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, p.ID, auth.ProjectID)
	assert.Equal(t, owner, auth.OwnerUserID, "a bearer request always acts as the project's single designated owner, not any other member")

	// The resolved owner's role-based checks all pass as owner.
	role, err := projects.Role(ctx, auth.OwnerUserID, auth.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, project.RoleOwner, role)
	assert.NoError(t, projects.Authorize(ctx, auth.OwnerUserID, auth.ProjectID, project.RoleOwner))
}

func TestService_Authenticate_RefreshesLastUsedAt(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	token, raw, err := svc.Create(ctx, owner, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)
	require.Nil(t, token.LastUsedAt)

	_, err = svc.Authenticate(ctx, raw)
	require.NoError(t, err)

	tokens, err := svc.List(ctx, owner, p.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.NotNil(t, tokens[0].LastUsedAt)
	assert.WithinDuration(t, time.Now(), *tokens[0].LastUsedAt, time.Minute)
}
