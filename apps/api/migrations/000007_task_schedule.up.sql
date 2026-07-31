-- Schedule/Gantt support (issue #33): a task's start date alongside its
-- existing due_on, plus predecessor/successor dependencies between tasks in
-- the same project. Neither is synced to GitLab — GitLab Issues have no
-- native "start date", so both stay app-only, unlike due_on.

ALTER TABLE tasks ADD COLUMN start_date DATE;

-- One row = "predecessor must finish before successor starts". Both tasks
-- always belong to the same project (enforced by internal/taskdependency,
-- not by a column here — task_dependencies has no project_id of its own,
-- the same way task_gitlab_links has no owner column, see CLAUDE.md's
-- per-project-scoping convention). Cycle prevention (a chain that loops back
-- to its own predecessor) cannot be expressed as a CHECK/UNIQUE constraint,
-- so it is enforced in the application layer instead (internal/taskdependency).
CREATE TABLE task_dependencies (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    predecessor_task_id UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    successor_task_id   UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (predecessor_task_id, successor_task_id),
    CHECK (predecessor_task_id <> successor_task_id)
);

CREATE INDEX idx_task_dependencies_predecessor ON task_dependencies(predecessor_task_id);
CREATE INDEX idx_task_dependencies_successor ON task_dependencies(successor_task_id);
