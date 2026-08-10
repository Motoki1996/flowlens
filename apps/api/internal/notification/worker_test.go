package notification_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/notification"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorker_Sweep_SendsOnceWhenPastSendHour(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	today := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC) // 10:00 UTC
	overdue := q.SeedTask(project.ID, owner.ID, "overdue task")
	overdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true}
	q.SetTaskForTest(overdue)

	_, err := q.UpsertNotificationSettings(context.Background(), db.UpsertNotificationSettingsParams{
		ProjectID:  project.ID,
		WebhookUrl: "https://hooks.example.com/acme",
		Enabled:    true,
		SendHour:   9, // already past by the 10:00 clock below
	})
	require.NoError(t, err)

	sender := &notification.FakeSender{}
	worker := notification.NewWorker(q, sender, notification.WithNowForTest(func() time.Time { return today }))

	worker.Sweep(context.Background())

	if assert.Len(t, sender.Sent, 1) {
		assert.Equal(t, "https://hooks.example.com/acme", sender.Sent[0].WebhookURL)
		assert.Len(t, sender.Sent[0].Digest.Overdue, 1)
	}
}

func TestWorker_Sweep_DoesNotSendTwiceOnTheSameDay(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	today := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	overdue := q.SeedTask(project.ID, owner.ID, "overdue task")
	overdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true}
	q.SetTaskForTest(overdue)

	_, err := q.UpsertNotificationSettings(context.Background(), db.UpsertNotificationSettingsParams{
		ProjectID:  project.ID,
		WebhookUrl: "https://hooks.example.com/acme",
		Enabled:    true,
		SendHour:   9,
	})
	require.NoError(t, err)

	sender := &notification.FakeSender{}
	worker := notification.NewWorker(q, sender, notification.WithNowForTest(func() time.Time { return today }))

	worker.Sweep(context.Background())
	worker.Sweep(context.Background()) // a second tick later the same day

	assert.Len(t, sender.Sent, 1, "a second sweep the same day must not send a duplicate digest")
}

func TestWorker_Sweep_DoesNotSendWhenNothingToReport(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	today := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	_, err := q.UpsertNotificationSettings(context.Background(), db.UpsertNotificationSettingsParams{
		ProjectID:  project.ID,
		WebhookUrl: "https://hooks.example.com/acme",
		Enabled:    true,
		SendHour:   9,
	})
	require.NoError(t, err)

	sender := &notification.FakeSender{}
	worker := notification.NewWorker(q, sender, notification.WithNowForTest(func() time.Time { return today }))

	worker.Sweep(context.Background())
	assert.Empty(t, sender.Sent, "a day with nothing overdue/due-soon/failed must not send")

	// An empty day must not have logged a digest either: once a task goes
	// overdue later the same day, a subsequent sweep must still be able to
	// send.
	overdue := q.SeedTask(project.ID, owner.ID, "now overdue")
	overdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true}
	q.SetTaskForTest(overdue)

	worker.Sweep(context.Background())
	assert.Len(t, sender.Sent, 1, "an empty first sweep must not have blocked a later non-empty sweep the same day")
}

func TestWorker_Sweep_DoesNotSendBeforeSendHour(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	today := time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC) // 05:00 UTC
	overdue := q.SeedTask(project.ID, owner.ID, "overdue task")
	overdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true}
	q.SetTaskForTest(overdue)

	_, err := q.UpsertNotificationSettings(context.Background(), db.UpsertNotificationSettingsParams{
		ProjectID:  project.ID,
		WebhookUrl: "https://hooks.example.com/acme",
		Enabled:    true,
		SendHour:   9, // not reached yet at 05:00
	})
	require.NoError(t, err)

	sender := &notification.FakeSender{}
	worker := notification.NewWorker(q, sender, notification.WithNowForTest(func() time.Time { return today }))

	worker.Sweep(context.Background())

	assert.Empty(t, sender.Sent, "send_hour not yet reached must not send")
}

func TestWorker_Sweep_IgnoresDisabledProjects(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("acme-owner", "owner@example.com")
	project := q.SeedProject(owner.ID, "acme")

	today := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	overdue := q.SeedTask(project.ID, owner.ID, "overdue task")
	overdue.DueOn = pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true}
	q.SetTaskForTest(overdue)

	_, err := q.UpsertNotificationSettings(context.Background(), db.UpsertNotificationSettingsParams{
		ProjectID:  project.ID,
		WebhookUrl: "https://hooks.example.com/acme",
		Enabled:    false,
		SendHour:   9,
	})
	require.NoError(t, err)

	sender := &notification.FakeSender{}
	worker := notification.NewWorker(q, sender, notification.WithNowForTest(func() time.Time { return today }))

	worker.Sweep(context.Background())

	assert.Empty(t, sender.Sent, "a disabled project must never send")
}
