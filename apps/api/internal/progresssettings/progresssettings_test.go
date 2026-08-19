package progresssettings_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/progresssettings"
	"github.com/flowlens/api/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_Get_ReturnsDefaultsWhenNeverSaved(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	p := q.SeedProject(owner.ID, "acme")
	svc := progresssettings.NewService(q, project.NewService(q))

	settings, err := svc.Get(context.Background(), owner.ID, p.ID)
	require.NoError(t, err)

	assert.False(t, settings.Enabled)
	assert.Equal(t, p.ID, settings.ProjectID)
}

func TestService_Save_RoundTrips(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	p := q.SeedProject(owner.ID, "acme")
	svc := progresssettings.NewService(q, project.NewService(q))

	saved, err := svc.Save(context.Background(), owner.ID, p.ID, true)
	require.NoError(t, err)
	assert.True(t, saved.Enabled)

	got, err := svc.Get(context.Background(), owner.ID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, saved, got)
}

func TestService_Save_NonOwnerForbidden(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	member := q.SeedUser("acme-member", "member@example.com")
	p := q.SeedProject(owner.ID, "acme")
	q.SeedProjectMember(p.ID, member.ID, "member")
	svc := progresssettings.NewService(q, project.NewService(q))

	_, err := svc.Save(context.Background(), member.ID, p.ID, true)
	assert.ErrorIs(t, err, progresssettings.ErrForbidden)
}
