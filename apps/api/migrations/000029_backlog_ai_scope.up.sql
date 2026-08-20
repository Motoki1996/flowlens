-- allowed_scope/forbidden_scope move from task_ai_contexts to backlogs: in
-- practice the paths a task may/may not touch describe a sub-area of the
-- codebase, not one unit of work, so they were being copy-pasted onto every
-- task in a backlog. They become app-only, agent-facing fields owned by the
-- backlog instead — the same shape as base_branch (see the 000024
-- migration) — resolved into GET /tasks/{taskID}/context through the
-- task's backlog. acceptanceCriteria/aiContext remain task-level.
ALTER TABLE backlogs ADD COLUMN allowed_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE backlogs ADD COLUMN forbidden_scope TEXT NOT NULL DEFAULT '';

ALTER TABLE task_ai_contexts DROP COLUMN allowed_scope;
ALTER TABLE task_ai_contexts DROP COLUMN forbidden_scope;
