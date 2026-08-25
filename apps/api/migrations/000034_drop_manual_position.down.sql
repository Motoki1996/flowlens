-- Re-adding the column restores the schema, not the data: the manual order
-- every row carried is gone, so every row comes back at the default 0 and
-- falls back to the created_at tiebreak the queries already applied.
ALTER TABLE tasks    ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE backlogs ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE epics    ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 0;
