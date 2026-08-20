-- task_ai_contexts is app-only: acceptance criteria and AI context must
-- never be sent to GitLab (see "Why the task is split across three tables"
-- in docs/plans/issue-sync.md). The allowed/forbidden change scope moved to
-- backlogs (000029 migration) since it describes a sub-area of the
-- codebase, not one task. Ownership is verified by the caller via
-- task.Service.Get before either query runs, the same way CreateTask
-- trusts an already-verified project.

-- name: UpsertTaskAIContext :one
INSERT INTO task_ai_contexts (task_id, acceptance_criteria, ai_context)
VALUES ($1, $2, $3)
ON CONFLICT (task_id) DO UPDATE
SET acceptance_criteria = EXCLUDED.acceptance_criteria,
    ai_context = EXCLUDED.ai_context,
    updated_at = now()
RETURNING task_id, acceptance_criteria, ai_context, updated_at;

-- name: GetTaskAIContext :one
SELECT task_id, acceptance_criteria, ai_context, updated_at
FROM task_ai_contexts
WHERE task_id = $1;
