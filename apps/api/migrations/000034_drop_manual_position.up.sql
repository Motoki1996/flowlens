-- Drop the manual drag-and-drop ordering column from every object that had
-- one. The reordering feature it existed for (PATCH .../tasks/order,
-- .../backlogs/order, .../epics/order, and the web app's drag-and-drop and
-- move-up/move-down controls) is removed: a list's default order is now
-- created_at, and the explicit ?sort= parameters (priority, progress,
-- dueOn, ...) remain the way to order a list deliberately.
ALTER TABLE tasks    DROP COLUMN IF EXISTS position;
ALTER TABLE backlogs DROP COLUMN IF EXISTS position;
ALTER TABLE epics    DROP COLUMN IF EXISTS position;
