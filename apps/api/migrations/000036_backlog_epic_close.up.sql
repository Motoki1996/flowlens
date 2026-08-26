-- A backlog's and an epic's own open/closed state.
--
-- A backlog that has shipped had nowhere to go. progress='done' (000011) says
-- "the work is finished", which is not the same statement as "this is no
-- longer something we are tracking" — a backlog that was abandoned rather
-- than delivered never reaches 'done' at all, and a delivered one stayed in
-- the collection view forever either way. That is the gap this closes.
--
-- Deliberately shaped as tasks.status/tasks.closed_at (000003) rather than a
-- single archived_at, because it is the same concept the task rung already
-- has and a second vocabulary for it would only have to be explained. The
-- one thing that does *not* carry over is provenance: tasks.status mirrors
-- the GitLab issue state and syncs both ways, whereas these two columns are
-- app-only end to end, like priority/progress/base_branch before them.
-- GitLab CE has no object either of these rungs maps to (see the 000032
-- comment on why the name Epic was free to take), so there is nothing to
-- sync with and closing one never enqueues an outbox job.
--
-- Closing does not cascade. An epic in a closed backlog, and a task in a
-- closed backlog or epic, keep the state they had: a parent's close is a
-- statement about the parent, and cascading it would either stamp closed_at
-- on tasks that were never finished — inventing completions in
-- internal/velocity, which reads closed_at as a completion signal — or write
-- a GitLab issue close FlowLens was never asked for. Leftover work is moved
-- to another backlog, not closed by proxy.
--
-- 'open' is the only value a row predating this migration can have, and it is
-- the right one, so there is nothing to backfill.
ALTER TABLE backlogs ADD COLUMN status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'closed'));
ALTER TABLE backlogs ADD COLUMN closed_at TIMESTAMPTZ;

ALTER TABLE epics ADD COLUMN status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'closed'));
ALTER TABLE epics ADD COLUMN closed_at TIMESTAMPTZ;

-- Both collections default to open-only (internal/backlog.ListFilter,
-- internal/epic.ListFilter resolve an absent ?status= to 'open'), which is
-- the whole point of the feature, so that is the path worth indexing.
CREATE INDEX idx_backlogs_project_id_status ON backlogs(project_id, status);
CREATE INDEX idx_epics_project_id_status    ON epics(project_id, status);
