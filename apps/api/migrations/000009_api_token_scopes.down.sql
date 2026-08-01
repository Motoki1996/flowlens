ALTER TABLE project_api_tokens DROP COLUMN token_prefix;

ALTER TABLE project_api_tokens DROP CONSTRAINT project_api_tokens_scopes_check;
ALTER TABLE project_api_tokens DROP COLUMN scopes;
