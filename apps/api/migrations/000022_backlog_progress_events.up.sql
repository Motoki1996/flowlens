-- An append-only log of backlogs.progress transitions (issue #173), the
-- same-shaped counterpart one level up from task_progress_events (000020).
-- It exists to answer two questions task-level events can't: how long a
-- backlog sits before someone starts it, and how long "task breakdown" (the
-- backlog going in_progress -> its first task appearing) takes — the step
-- an AI agent is meant to own in this flow, per issue #173's design.
--
-- A row is written only by internal/backlog.Service.Update, and only when
-- progress actually changes; the first stage's start is backlogs.created_at
-- itself, so backlog creation writes no row here. actor_kind/actor_user_id
-- follow task_progress_events' own convention (see its migration's
-- comment).
CREATE TABLE backlog_progress_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    backlog_id    UUID NOT NULL REFERENCES backlogs(id) ON DELETE CASCADE,
    from_progress TEXT NOT NULL,
    to_progress   TEXT NOT NULL,
    actor_kind    TEXT NOT NULL CHECK (actor_kind IN ('user', 'agent')),
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_backlog_progress_events_backlog_id ON backlog_progress_events(backlog_id, occurred_at);
