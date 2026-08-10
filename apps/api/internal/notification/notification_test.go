package notification_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/notification"
	"github.com/flowlens/api/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_Get_ReturnsDefaultsWhenNeverSaved(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	p := q.SeedProject(owner.ID, "acme")
	svc := notification.NewService(q, project.NewService(q))

	settings, err := svc.Get(context.Background(), owner.ID, p.ID)
	require.NoError(t, err)

	assert.False(t, settings.Enabled)
	assert.Equal(t, notification.DefaultSendHour, settings.SendHour)
}

func TestService_Save_RoundTrips(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	p := q.SeedProject(owner.ID, "acme")
	svc := notification.NewService(q, project.NewService(q))

	saved, err := svc.Save(context.Background(), owner.ID, p.ID, "https://hooks.example.com/acme", true, 14)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example.com/acme", saved.WebhookURL)
	assert.True(t, saved.Enabled)
	assert.Equal(t, 14, saved.SendHour)

	got, err := svc.Get(context.Background(), owner.ID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, saved, got)
}

func TestService_Save_RejectsInvalidWebhookURLWhenEnabled(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	p := q.SeedProject(owner.ID, "acme")
	svc := notification.NewService(q, project.NewService(q))

	_, err := svc.Save(context.Background(), owner.ID, p.ID, "not-a-url", true, 9)
	assert.ErrorIs(t, err, notification.ErrInvalidWebhookURL)
}

func TestService_Save_RejectsInvalidSendHour(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	p := q.SeedProject(owner.ID, "acme")
	svc := notification.NewService(q, project.NewService(q))

	_, err := svc.Save(context.Background(), owner.ID, p.ID, "https://hooks.example.com/acme", true, 24)
	assert.ErrorIs(t, err, notification.ErrInvalidSendHour)
}

func TestService_Save_NonOwnerForbidden(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	member := q.SeedUser("acme-member", "member@example.com")
	p := q.SeedProject(owner.ID, "acme")
	q.SeedProjectMember(p.ID, member.ID, "member")
	svc := notification.NewService(q, project.NewService(q))

	_, err := svc.Save(context.Background(), member.ID, p.ID, "https://hooks.example.com/acme", true, 9)
	assert.ErrorIs(t, err, notification.ErrForbidden)
}
