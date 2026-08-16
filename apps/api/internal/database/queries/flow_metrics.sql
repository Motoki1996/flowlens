-- Flow-metrics aggregation (issue #171): per-task stage lead times, sourced
-- from task_progress_events (issue #169) plus each task's earliest linked
-- merge request (merge_requests.task_id, set by internal/mrsync, issue
-- #111). Unlike deliverymetrics, which aggregates merge_requests alone,
-- this walks the whole progress pipeline a task moves through — see
-- internal/flowmetrics for how the two queries below are combined.

-- ListTasksForFlowMetrics returns each in-range task alongside its earliest
-- linked merge request's gitlab_created_at/merged_at (NULL when the task
-- has no linked merge request, or none yet has a GitLab-side created_at). A
-- merge request has at most one task (migration 000019's comment), but a
-- task can have more than one merge request; the earliest by
-- gitlab_created_at is the one flow-metrics' implementation/review stages
-- measure against, since it's the one that closes the "in_progress" wait.

-- name: ListTasksForFlowMetrics :many
SELECT t.id, t.created_at, mr.gitlab_created_at AS mr_gitlab_created_at, mr.merged_at AS mr_merged_at
FROM tasks t
LEFT JOIN LATERAL (
    SELECT m.gitlab_created_at, m.merged_at
    FROM merge_requests m
    WHERE m.task_id = t.id
    ORDER BY m.gitlab_created_at ASC NULLS LAST, m.created_at ASC
    LIMIT 1
) mr ON true
WHERE t.project_id = sqlc.arg(project_id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND (sqlc.narg(since)::timestamptz IS NULL OR t.created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR t.created_at <= sqlc.narg(until));

-- ListTaskProgressEventsForFlowMetrics returns every task_progress_events
-- row for tasks in the same project/date range ListTasksForFlowMetrics
-- selects, so internal/flowmetrics can replay each task's progress
-- timeline (first in_progress, first done, on_hold intervals) without an
-- N+1 query per task. Ordered by task then occurred_at so the caller can
-- walk each task's timeline in order without re-sorting.

-- name: ListTaskProgressEventsForFlowMetrics :many
SELECT e.task_id, e.from_progress, e.to_progress, e.occurred_at
FROM task_progress_events e
JOIN tasks t ON t.id = e.task_id
WHERE t.project_id = sqlc.arg(project_id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND (sqlc.narg(since)::timestamptz IS NULL OR t.created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR t.created_at <= sqlc.narg(until))
ORDER BY e.task_id, e.occurred_at ASC;
