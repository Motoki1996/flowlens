ALTER TABLE task_progress_events DROP CONSTRAINT task_progress_events_actor_kind_check;
ALTER TABLE task_progress_events ADD CONSTRAINT task_progress_events_actor_kind_check
    CHECK (actor_kind IN ('user', 'agent'));

DROP TABLE IF EXISTS progress_sync_settings;
