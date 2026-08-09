-- Project membership (docs/decisions/0010-why-project-membership.md). Schema
-- only: no handler or query in this branch changes what the app does. The
-- ownership-check replacement (owner_user_id equality -> membership) is a
-- follow-up issue.

CREATE TABLE project_members (
    project_id UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    role       TEXT        NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member', 'viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX idx_project_members_user_id ON project_members(user_id);

-- Every existing project's owner_user_id becomes an 'owner' membership row,
-- so the invariant "every project has an owner membership row" holds from
-- the moment this migration lands, not just for projects created after it.
INSERT INTO project_members (project_id, user_id, role)
SELECT id, owner_user_id, 'owner' FROM projects;
