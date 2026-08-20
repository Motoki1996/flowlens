-- Project invites (issue #211). Adding someone to a project has so far
-- required them to already have an account (internal/projectmember's Add
-- resolves a username/email against users), and the only way to get an
-- account is POST /auth/signup — which docs/self-hosting.md tells operators
-- to close with ALLOW_SIGNUP=false. The two together mean a hardened
-- instance cannot onboard anyone. An invite is the credential that reopens
-- that door for one named person, once, without reopening registration for
-- everyone.
--
-- token_hash follows sessions and project_api_tokens: the raw value is
-- returned exactly once, at creation, and only its SHA-256 hash is stored,
-- so a database leak yields no usable invite. token_prefix keeps the
-- leading characters for a list UI to tell invites apart, the same split
-- project_api_tokens uses (000009).
CREATE TABLE project_invites (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    token_hash          TEXT        NOT NULL UNIQUE,
    token_prefix        TEXT        NOT NULL,
    -- The role the invitee is given on acceptance, from the same vocabulary
    -- project_members.role uses (000012).
    role                TEXT        NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member', 'viewer')),
    -- Invites are single-use: accepted_at is what spends one, and every
    -- lookup requires it to be NULL. accepted_by_user_id records who spent
    -- it, so an owner can see the invite they sent became a specific member.
    accepted_at         TIMESTAMPTZ,
    accepted_by_user_id UUID        REFERENCES users(id) ON DELETE SET NULL,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_by_user_id  UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_invites_project_id ON project_invites(project_id);
