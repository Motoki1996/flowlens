-- An append-only log of tasks.progress transitions (issue #169). progress
-- itself (000011) only ever holds the current value, and updated_at is
-- overwritten by any edit (e.g. a title change), so neither can answer "how
-- long was this task in_progress" — the question this table exists to make
-- answerable, starting from whenever this migration ships (there is no way
-- to backfill history that was never recorded).
--
-- A row is written only by internal/task.Service.Update, and only when
-- progress actually changes; the first stage's start is tasks.created_at
-- itself, so task creation writes no row here. actor_kind follows
-- task_comments.author_kind's vocabulary (minus 'gitlab': progress is
-- app-only and never moves via the GitLab sync path) so a progress change
-- driven by an AI agent can be told apart from one a human made.
-- actor_user_id is set only for actor_kind='user' — an 'agent' change is
-- attributed to the project API token, which this table does not otherwise
-- track.
CREATE TABLE task_progress_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    from_progress TEXT NOT NULL,
    to_progress   TEXT NOT NULL,
    actor_kind    TEXT NOT NULL CHECK (actor_kind IN ('user', 'agent')),
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_progress_events_task_id ON task_progress_events(task_id, occurred_at);
