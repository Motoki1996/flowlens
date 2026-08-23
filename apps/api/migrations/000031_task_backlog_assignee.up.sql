-- A task's and a backlog's own FlowLens assignee: which *FlowLens user*
-- (project_members) is responsible for the work, as opposed to
-- tasks.assignee_gitlab_user_id (000003), which mirrors the GitLab issue's
-- assignee and syncs both ways.
--
-- The two coexist deliberately. assignee_user_id is app-only and is never
-- written by the inbound sync path (internal/webhookapply,
-- internal/projectsync): a GitLab-side assignee change moves the gitlab
-- columns only, so the FlowLens assignee survives as a record of what a
-- human actually decided. The outbound direction is a one-way bridge —
-- internal/task resolves the assignee's user_gitlab_identities row for the
-- project's GitLab connection and, when one exists, sets the gitlab columns
-- in the same write, which is what puts the change on the issue. A user with
-- no identity registered is still a perfectly good assignee; the task is
-- simply assigned inside FlowLens only.
--
-- backlogs get the same column and the same meaning ("who owns this backlog"),
-- minus the bridge: a backlog has no GitLab counterpart to mirror to, so it
-- is app-only end to end, like base_branch (000024) and allowed_scope (000029).
--
-- ON DELETE SET NULL, not CASCADE: a user being deleted must unassign their
-- work, never delete it — the same rule task_comments.author_user_id follows.
ALTER TABLE tasks    ADD COLUMN assignee_user_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE backlogs ADD COLUMN assignee_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_tasks_project_id_assignee_user_id    ON tasks(project_id, assignee_user_id);
CREATE INDEX idx_backlogs_project_id_assignee_user_id ON backlogs(project_id, assignee_user_id);

-- Backfill: a task already assigned to a GitLab user who is both a member of
-- the task's project and has that GitLab identity registered is, unambiguously,
-- assigned to that FlowLens user. Without this every pre-existing task would
-- read as unassigned on the new column. The project_members join matters —
-- Service.resolveAssigneeUser rejects a non-member assignee, so backfilling one
-- would write a value the API itself would refuse on the next PATCH.
--
-- Tasks whose GitLab assignee maps to nobody are left NULL, and the
-- ?assignee=me filter still matches them through the gitlab column (it ORs the
-- two), so an instance that skips this backfill loses nothing but display.
UPDATE tasks t
SET assignee_user_id = ugi.user_id
FROM gitlab_connections gc
JOIN user_gitlab_identities ugi ON ugi.gitlab_base_url = gc.base_url
JOIN project_members pm ON pm.project_id = gc.project_id AND pm.user_id = ugi.user_id
WHERE gc.project_id = t.project_id
  AND t.assignee_gitlab_user_id IS NOT NULL
  AND t.assignee_gitlab_user_id = ugi.gitlab_user_id;
