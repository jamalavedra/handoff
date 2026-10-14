-- agent_webhooks: where to POST an agent's notifications. secret is sealed with HANDOFF_SECRET and
-- signs every delivery; only notifications created after `since` are sent.
CREATE TABLE agent_webhooks (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    agent_id    uuid NOT NULL,
    url         text NOT NULL,
    secret      bytea NOT NULL,
    since       timestamptz NOT NULL DEFAULT now(),
    last_at     timestamptz,
    last_status int,
    last_error  text NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, agent_id),
    FOREIGN KEY (org_id, agent_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

-- webhook_deliveries: one row per notification to send, with retries.
CREATE TABLE webhook_deliveries (
    org_id          uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    notification_id bigint NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    agent_id        uuid NOT NULL,
    attempts        int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    delivered_at    timestamptz,
    failed_at       timestamptz,
    last_error      text NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, notification_id),
    FOREIGN KEY (org_id, agent_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);
CREATE INDEX webhook_deliveries_pending ON webhook_deliveries (org_id, next_attempt_at)
    WHERE delivered_at IS NULL AND failed_at IS NULL;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['agent_webhooks', 'webhook_deliveries']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY org_isolation ON %I
            USING (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)
            WITH CHECK (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)', t);
    END LOOP;
END $$;

-- due: a task assigned to an agent has come due (recurring tasks make this a schedule).
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN ('mention', 'assigned', 'approval', 'due'));
