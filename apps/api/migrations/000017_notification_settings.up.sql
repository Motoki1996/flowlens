-- Daily digest notifications (issue #109): overdue tasks, tasks due within
-- 24h, and failed sync jobs / webhook events, delivered by an outgoing
-- webhook rather than email (agreed on the issue: no SMTP dependency, and
-- it plugs straight into Slack via an Incoming Webhook URL).

CREATE TABLE notification_settings (
    project_id UUID        NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    webhook_url TEXT       NOT NULL DEFAULT '',
    enabled     BOOLEAN    NOT NULL DEFAULT false,
    -- Hour of day (UTC) the daily digest worker sends this project's digest.
    send_hour   INTEGER    NOT NULL DEFAULT 9 CHECK (send_hour >= 0 AND send_hour <= 23),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id)
);

-- One row per project per calendar day a digest was attempted. The unique
-- constraint is the dedupe guard itself: the worker inserts here *before*
-- sending, so a second worker tick (or process) racing the same project/day
-- fails the insert and skips rather than sending twice.
CREATE TABLE notification_digests (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    digest_date DATE        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'sent' CHECK (status IN ('sent', 'failed')),
    error       TEXT        NOT NULL DEFAULT '',
    sent_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, digest_date)
);
