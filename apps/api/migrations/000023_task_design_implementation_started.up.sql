-- Explicit spec-driven-development phase markers: unlike every other
-- flow-metrics timestamp, which is derived from task_progress_events'
-- in_progress/done transitions, these two are written directly by whoever
-- (an AI agent or a human) starts that phase, via POST
-- .../design-started and .../implementation-started. Both are app-only,
-- never synced to GitLab, and overwritable — calling the endpoint again
-- (e.g. redoing the design after a review comment) just moves the
-- timestamp forward, there is no "already set" guard. Independent of each
-- other: a task with implementation_started_at but no design_started_at
-- simply skipped the design phase, and internal/flowmetrics excludes a
-- stage rather than measuring it against a missing endpoint.
ALTER TABLE tasks ADD COLUMN design_started_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN implementation_started_at TIMESTAMPTZ;
