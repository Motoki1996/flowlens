// Package progresssync implements issue #202: on a project that has opted
// in (internal/progresssettings), a task's GitLab issue closing also moves
// its progress to 'done', recording the change as an
// ActorKindGitlab task_progress_events row so it's auditable who (or what)
// moved it. This is a deliberate, opt-in exception to the invariant
// documented on task.Progress — progress is otherwise app-only and never
// written by the GitLab sync path.
package progresssync

import (
	"context"
	"fmt"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ApplyOnClose is called by internal/webhookapply and internal/projectsync
// right after they write a task's status, inside the same transaction. It
// is a no-op unless all of the following hold:
//   - newStatus is task.StatusClosed
//   - previousStatus was not already task.StatusClosed (a genuine open->
//     closed transition, not a re-applied or redelivered "already closed"
//     update — so a redelivered webhook never appends a second event, and a
//     progress a human has since moved off 'done' is never clobbered by a
//     stale re-apply)
//   - previousProgress is not already task.ProgressDone
//   - the project has progress sync enabled (internal/progresssettings)
//
// previousStatus/previousProgress must be read by the caller before its own
// status write, in the same transaction q belongs to.
func ApplyOnClose(ctx context.Context, q db.Querier, projectID, taskID uuid.UUID, previousStatus, newStatus, previousProgress string) error {
	if newStatus != task.StatusClosed || previousStatus == task.StatusClosed || previousProgress == task.ProgressDone {
		return nil
	}

	enabled, err := q.IsProgressSyncEnabledForProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("progresssync: check settings: %w", err)
	}
	if !enabled {
		return nil
	}

	if _, err := q.ApplyGitlabProgressDone(ctx, taskID); err != nil {
		return fmt.Errorf("progresssync: set progress done: %w", err)
	}
	if _, err := q.CreateTaskProgressEvent(ctx, db.CreateTaskProgressEventParams{
		TaskID:       taskID,
		FromProgress: previousProgress,
		ToProgress:   task.ProgressDone,
		ActorKind:    task.ActorKindGitlab,
		ActorUserID:  pgtype.UUID{},
	}); err != nil {
		return fmt.Errorf("progresssync: record progress event: %w", err)
	}
	return nil
}
