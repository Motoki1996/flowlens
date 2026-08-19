-- Velocity aggregation (issue #195): throughput (completed tasks per
-- period), not lead time — see internal/velocity for how the two queries
-- below are combined into a completion time per task and bucketed.

-- ListTaskCompletionsForVelocity returns one row per in-project task,
-- carrying both possible completion signals so internal/velocity can pick
-- the earlier of the two (never both): tasks.closed_at (set when a linked
-- GitLab issue closes, which never touches tasks.progress — see
-- UpsertTaskFromGitlabIssue in tasks.sql) and the occurred_at/actor_kind of
-- the task's *first* transition to progress='done' (task_progress_events
-- only exists from migration 000020 on, so a task done before that has no
-- such row and is only reachable via closed_at). since/until are
-- deliberately not applied here: a completion time is derived from two
-- columns in Go, so filtering has to happen after that derivation, not in
-- SQL.
--
-- name: ListTaskCompletionsForVelocity :many
-- done_actor_kind is COALESCEd to '' rather than left NULL: sqlc's static
-- nullability inference doesn't see through the LATERAL join and would
-- otherwise generate a non-nullable Go string that panics scanning a real
-- NULL. internal/velocity only reads it when done_occurred_at is valid, so
-- the '' case (no done transition) is never looked at.
SELECT t.id AS task_id, t.size, t.closed_at, done.occurred_at AS done_occurred_at, COALESCE(done.actor_kind, '') AS done_actor_kind
FROM tasks t
LEFT JOIN LATERAL (
    SELECT e.occurred_at, e.actor_kind
    FROM task_progress_events e
    WHERE e.task_id = t.id AND e.to_progress = 'done'
    ORDER BY e.occurred_at ASC
    LIMIT 1
) done ON true
WHERE t.project_id = sqlc.arg(project_id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- CountOpenTasksBySizeForVelocity returns projectID's current count of
-- not-yet-completed tasks (status='open' AND progress<>'done') grouped by
-- size, used to forecast how many periods the remaining work will take at
-- the recent velocity. Always the current counts, regardless of any
-- ?from=/?to= the request passed.
--
-- It groups by size rather than returning a bare COUNT(*) plus a
-- SUM(CASE size ...) so that the size->points weight table lives in exactly
-- one place, internal/velocity.sizePoints. Summing here would mean the same
-- weights written twice, in Go and in SQL, free to drift apart. The total
-- open count is just the sum of these rows' counts.
--
-- name: CountOpenTasksBySizeForVelocity :many
SELECT t.size, COUNT(*) AS count FROM tasks t
WHERE t.project_id = sqlc.arg(project_id)
  AND t.status = 'open'
  AND t.progress <> 'done'
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
GROUP BY t.size;
