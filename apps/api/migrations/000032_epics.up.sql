-- Epics: an optional layer between a backlog and its tasks.
--
-- A refined backlog is not broken straight down into implementation-sized
-- tasks in practice. The work is first cut into coarse units — one screen,
-- one endpoint group, one migration+API pair — and only then is each coarse
-- unit broken into tasks someone actually works. That coarse unit had
-- nowhere to live: it became either a task with no code behind it (which
-- pollutes internal/velocity and the Board) or a numbered requirement buried
-- in the backlog's description (which cannot carry a base branch, an
-- assignee or dates).
--
-- The rung is optional in both directions: a task may sit directly in a
-- backlog exactly as before (epic_id NULL), and an epic may sit outside any
-- backlog (backlog_id NULL, the Unclassified group).
--
-- App-only, like backlogs: no epic is ever created in, or read from, GitLab.
-- GitLab CE has no Epic at all (it is a Premium object), which is also why
-- the name is free to take here — contrast Milestone, which does exist in CE
-- and would collide with a future milestone sync.
--
-- Every column below already exists on backlogs with the same meaning. That
-- is deliberate: an epic is "a backlog that lives inside a backlog", which is
-- what lets internal/task resolve base_branch/allowed_scope/forbidden_scope
-- across the two rungs with one query and lets the web app reuse the Backlog
-- collection sections. The one field an epic deliberately does *not* get is
-- size — an epic's size is the sum of its tasks', the same reason a backlog
-- has none.
CREATE TABLE epics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- NULL = Unclassified (unfiled); a backlog being deleted must not delete
    -- its epics, exactly as tasks.backlog_id (000003) behaves.
    backlog_id  UUID        REFERENCES backlogs(id) ON DELETE SET NULL,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    position    INTEGER     NOT NULL DEFAULT 0,
    start_date  DATE,
    due_on      DATE,
    priority    TEXT        NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    progress    TEXT        NOT NULL DEFAULT 'not_started' CHECK (progress IN ('not_started', 'in_progress', 'on_hold', 'done')),
    -- app-only, no GitLab bridge — a backlog's rule (000031), not a task's.
    assignee_user_id UUID   REFERENCES users(id) ON DELETE SET NULL,
    -- The field this layer exists for: the branch tasks in this epic are
    -- meant to branch from. Resolved epic-first, backlog-second into
    -- GET /api/v1/tasks/{taskID}/context. Never synced to GitLab (000024).
    base_branch     TEXT    NOT NULL DEFAULT '',
    -- The paths tasks in this epic may/may not touch, resolved the same way
    -- (000029).
    allowed_scope   TEXT    NOT NULL DEFAULT '',
    forbidden_scope TEXT    NOT NULL DEFAULT '',
    -- The epic's own destination for new issues, overriding the backlog's own
    -- override of the project default (000021). Read only when a task is
    -- created; task_gitlab_links governs every later update, so moving a task
    -- between epics never moves or re-targets an issue that already exists.
    -- Nothing in the schema constrains this to the project's own GitLab
    -- connection — that check lives in internal/epic, as it does in
    -- internal/backlog.
    default_linked_gitlab_project_id UUID REFERENCES linked_gitlab_projects(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_epics_project_id                  ON epics(project_id);
CREATE INDEX idx_epics_project_id_backlog_id       ON epics(project_id, backlog_id);
CREATE INDEX idx_epics_project_id_assignee_user_id ON epics(project_id, assignee_user_id);
CREATE INDEX idx_epics_default_linked_gitlab_project
    ON epics(default_linked_gitlab_project_id)
    WHERE default_linked_gitlab_project_id IS NOT NULL;

-- NULL — the only value a task predating this migration can have — means
-- "filed directly in a backlog", which is exactly what those tasks are, so
-- there is nothing to backfill. ON DELETE SET NULL for the same reason
-- backlog_id has it: deleting an epic unfiles its tasks, never deletes them.
--
-- A task's epic_id and backlog_id must agree (a task's epic must belong to
-- the task's backlog). The schema cannot express that either; internal/task
-- enforces it by writing backlog_id from the epic in the same statement.
ALTER TABLE tasks ADD COLUMN epic_id UUID REFERENCES epics(id) ON DELETE SET NULL;

CREATE INDEX idx_tasks_project_id_epic_id ON tasks(project_id, epic_id);
