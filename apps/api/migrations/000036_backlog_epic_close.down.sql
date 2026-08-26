DROP INDEX IF EXISTS idx_epics_project_id_status;
DROP INDEX IF EXISTS idx_backlogs_project_id_status;

ALTER TABLE epics DROP COLUMN closed_at;
ALTER TABLE epics DROP COLUMN status;

ALTER TABLE backlogs DROP COLUMN closed_at;
ALTER TABLE backlogs DROP COLUMN status;
