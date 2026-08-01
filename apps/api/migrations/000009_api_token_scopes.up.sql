-- API tokens currently reach only two read-only endpoints, so scope has
-- never mattered. Before any write endpoint is opened to bearer auth, tokens
-- need a declared permission set; a bare boolean would not leave room for a
-- future scope beyond read/write.
--
-- token_prefix stores the first few characters of the raw token (e.g.
-- "flt_a1b2") so a token list UI can tell issued tokens apart. The column is
-- separate from token_hash, which stays a one-way hash of the full value.
ALTER TABLE project_api_tokens ADD COLUMN scopes TEXT[] NOT NULL DEFAULT '{read}';
ALTER TABLE project_api_tokens ADD CONSTRAINT project_api_tokens_scopes_check
    CHECK (scopes <@ ARRAY['read', 'write'] AND array_length(scopes, 1) >= 1);

ALTER TABLE project_api_tokens ADD COLUMN token_prefix TEXT;
