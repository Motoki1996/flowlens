-- Progress sync on issue close (issue #202): a per-project, owner-only
-- opt-in that lets a GitLab issue closing move its task's progress to
-- 'done', which the app side otherwise never touches from the sync path
-- (see the 000011 migration). Off by default so no existing project's
-- behavior changes until an owner turns it on. One row per project, the
-- same "settings conceptually always exist, just possibly unset" shape as
-- notification_settings (000017).
CREATE TABLE progress_sync_settings (
    project_id UUID       NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    enabled    BOOLEAN    NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id)
);

-- Widen task_progress_events.actor_kind (000020) to allow 'gitlab': the
-- actor a progress-sync-triggered event is attributed to, distinct from
-- 'user'/'agent' since no acting user exists on the inbound sync path.
ALTER TABLE task_progress_events DROP CONSTRAINT task_progress_events_actor_kind_check;
ALTER TABLE task_progress_events ADD CONSTRAINT task_progress_events_actor_kind_check
    CHECK (actor_kind IN ('user', 'agent', 'gitlab'));
