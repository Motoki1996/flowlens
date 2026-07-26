-- task_ai_contexts is app-only: acceptance criteria, AI context, and the
-- allowed/forbidden change scope must never be sent to GitLab (see "Why the
-- task is split across three tables" in docs/plans/issue-sync.md). Ownership
-- is verified by the caller via task.Service.Get before either query runs,
-- the same way CreateTask trusts an already-verified project.

-- name: UpsertTaskAIContext :one
INSERT INTO task_ai_contexts (task_id, acceptance_criteria, ai_context, allowed_scope, forbidden_scope)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (task_id) DO UPDATE
SET acceptance_criteria = EXCLUDED.acceptance_criteria,
    ai_context = EXCLUDED.ai_context,
    allowed_scope = EXCLUDED.allowed_scope,
    forbidden_scope = EXCLUDED.forbidden_scope,
    updated_at = now()
RETURNING task_id, acceptance_criteria, ai_context, allowed_scope, forbidden_scope, updated_at;

-- name: GetTaskAIContext :one
SELECT task_id, acceptance_criteria, ai_context, allowed_scope, forbidden_scope, updated_at
FROM task_ai_contexts
WHERE task_id = $1;
