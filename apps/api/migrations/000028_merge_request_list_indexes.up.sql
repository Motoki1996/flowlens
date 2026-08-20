-- Indexes backing the MergeRequest collection view's paged reads (issue
-- #112's list, now LIMIT/OFFSET paged). Before this, the only indexes on
-- merge_requests were repository_id, state and task_id, none of which can
-- serve the view's ORDER BY: it ranks by gitlab_created_at or
-- gitlab_updated_at DESC, so every page cost a full sort of the table (at
-- 50k merge requests, a sequential scan plus a top-N sort).
--
-- The sort key leads each index because the query has no equality predicate
-- on repository_id — the project scope arrives through a join, not a WHERE
-- clause, so an index leading with repository_id is never chosen. Measured
-- against 50k rows: 54ms -> 0.16ms for "all states", 15ms -> 0.29ms for the
-- view's default (state=opened, sort=updated).
--
-- Four indexes, because the view's two sorts each need one form with the
-- state filter and one without: a state-leading index can't serve a query
-- with no state predicate, since its second column is only ordered within
-- one state. DESC NULLS LAST matches the ORDER BY exactly (a merge request
-- whose GitLab timestamp didn't parse sorts last, see issue #183); an index
-- whose null ordering disagrees can't serve the sort.
--
-- Plain CREATE INDEX, not CONCURRENTLY: golang-migrate runs each file in a
-- transaction, which CONCURRENTLY cannot join. It takes a write lock on
-- merge_requests for the duration, during which the API is not yet serving
-- (migrations run at startup) — an upgrade pause, not an outage.

CREATE INDEX idx_merge_requests_state_created
    ON merge_requests (state, gitlab_created_at DESC NULLS LAST, created_at DESC);

CREATE INDEX idx_merge_requests_state_updated
    ON merge_requests (state, gitlab_updated_at DESC NULLS LAST, created_at DESC);

CREATE INDEX idx_merge_requests_created
    ON merge_requests (gitlab_created_at DESC NULLS LAST, created_at DESC);

CREATE INDEX idx_merge_requests_updated
    ON merge_requests (gitlab_updated_at DESC NULLS LAST, created_at DESC);

-- idx_merge_requests_state is now redundant: idx_merge_requests_state_created
-- has state as its leading column, so it answers everything the single-column
-- index did.
DROP INDEX idx_merge_requests_state;
