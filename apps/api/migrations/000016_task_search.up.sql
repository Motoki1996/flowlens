-- Full-text search over tasks (issue #106). search_vector is a STORED
-- generated column so it is indexable and never falls out of sync with
-- title/description, at the cost of dictionary-free text search: it uses
-- the 'simple' text search configuration (no stemming, no dictionary), so
-- Japanese task titles/descriptions work as long as the query matches a
-- whole contiguous run of characters the parser tokenizes as one lexeme —
-- there is no morphological word segmentation. pg_bigm/pgroonga would add
-- that, but neither is worth the extra dependency for this feature yet.
ALTER TABLE tasks ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple', title || ' ' || description)
    ) STORED;

CREATE INDEX idx_tasks_search_vector ON tasks USING GIN (search_vector);
