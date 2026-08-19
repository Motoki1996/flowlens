package progresssync_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/progresssync"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTaskService(q *dbtest.FakeQuerier) *task.Service {
	projects := project.NewService(q)
	backlogs := backlog.NewService(q, dbtest.FakeTxRunner{Q: q}, projects)
	return task.NewService(q, dbtest.FakeTxRunner{Q: q}, projects, backlogs)
}

func TestApplyOnClose_TableDriven(t *testing.T) {
	cases := []struct {
		name              string
		settingEnabled    bool
		previousStatus    string
		newStatus         string
		previousProgress  string
		wantProgress      string
		wantEventRecorded bool
	}{
		{
			name:              "setting off: open->closed leaves progress alone",
			settingEnabled:    false,
			previousStatus:    task.StatusOpen,
			newStatus:         task.StatusClosed,
			previousProgress:  task.ProgressInProgress,
			wantProgress:      task.ProgressInProgress,
			wantEventRecorded: false,
		},
		{
			name:              "setting on, genuine open->closed transition: progress moves to done",
			settingEnabled:    true,
			previousStatus:    task.StatusOpen,
			newStatus:         task.StatusClosed,
			previousProgress:  task.ProgressInProgress,
			wantProgress:      task.ProgressDone,
			wantEventRecorded: true,
		},
		{
			name:              "setting on, re-applying an already-closed status: no-op",
			settingEnabled:    true,
			previousStatus:    task.StatusClosed,
			newStatus:         task.StatusClosed,
			previousProgress:  task.ProgressInProgress,
			wantProgress:      task.ProgressInProgress,
			wantEventRecorded: false,
		},
		{
			name:              "setting on, progress already done: no-op, no duplicate event",
			settingEnabled:    true,
			previousStatus:    task.StatusOpen,
			newStatus:         task.StatusClosed,
			previousProgress:  task.ProgressDone,
			wantProgress:      task.ProgressDone,
			wantEventRecorded: false,
		},
		{
			name:              "setting on, reopen (closed->open): never applies, one-directional only",
			settingEnabled:    true,
			previousStatus:    task.StatusClosed,
			newStatus:         task.StatusOpen,
			previousProgress:  task.ProgressDone,
			wantProgress:      task.ProgressDone,
			wantEventRecorded: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := dbtest.New()
			owner := q.SeedUser("octocat", "octocat@example.com")
			p := q.SeedProject(owner.ID, "Alpha")
			tasks := newTaskService(q)
			tsk, err := tasks.Create(context.Background(), owner.ID, p.ID, task.CreateParams{Title: "Task", Progress: c.previousProgress})
			require.NoError(t, err)

			if c.settingEnabled {
				_, err := q.UpsertProgressSyncSettings(context.Background(), db.UpsertProgressSyncSettingsParams{
					ProjectID: p.ID,
					Enabled:   true,
				})
				require.NoError(t, err)
			}

			err = progresssync.ApplyOnClose(context.Background(), q, p.ID, tsk.ID, c.previousStatus, c.newStatus, c.previousProgress)
			require.NoError(t, err)

			got, err := q.GetTaskForProject(context.Background(), db.GetTaskForProjectParams{ID: tsk.ID, ProjectID: p.ID})
			require.NoError(t, err)
			assert.Equal(t, c.wantProgress, got.Progress)

			events, err := q.ListTaskProgressEventsByTask(context.Background(), tsk.ID)
			require.NoError(t, err)
			if c.wantEventRecorded {
				require.Len(t, events, 1)
				assert.Equal(t, task.ActorKindGitlab, events[0].ActorKind)
				assert.Equal(t, c.previousProgress, events[0].FromProgress)
				assert.Equal(t, task.ProgressDone, events[0].ToProgress)
				assert.False(t, events[0].ActorUserID.Valid)
			} else {
				assert.Empty(t, events)
			}
		})
	}
}

func TestApplyOnClose_DuplicateDeliveryNeverAppendsSecondEvent(t *testing.T) {
	q := dbtest.New()
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tasks := newTaskService(q)
	tsk, err := tasks.Create(context.Background(), owner.ID, p.ID, task.CreateParams{Title: "Task"})
	require.NoError(t, err)
	_, err = q.UpsertProgressSyncSettings(context.Background(), db.UpsertProgressSyncSettingsParams{ProjectID: p.ID, Enabled: true})
	require.NoError(t, err)

	// First delivery: a genuine open->closed transition.
	err = progresssync.ApplyOnClose(context.Background(), q, p.ID, tsk.ID, task.StatusOpen, task.StatusClosed, task.ProgressNotStarted)
	require.NoError(t, err)

	// Redelivered webhook for the same close: caller now reads previousStatus
	// as already closed, since the first call already committed it.
	err = progresssync.ApplyOnClose(context.Background(), q, p.ID, tsk.ID, task.StatusClosed, task.StatusClosed, task.ProgressDone)
	require.NoError(t, err)

	events, err := q.ListTaskProgressEventsByTask(context.Background(), tsk.ID)
	require.NoError(t, err)
	assert.Len(t, events, 1)
}
