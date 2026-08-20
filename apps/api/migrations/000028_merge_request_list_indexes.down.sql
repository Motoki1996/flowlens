CREATE INDEX idx_merge_requests_state ON merge_requests(state);

DROP INDEX IF EXISTS idx_merge_requests_updated;
DROP INDEX IF EXISTS idx_merge_requests_created;
DROP INDEX IF EXISTS idx_merge_requests_state_updated;
DROP INDEX IF EXISTS idx_merge_requests_state_created;
