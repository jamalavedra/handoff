-- app_actions: things the agent prepared in a person's apps (an email, a post, an event) that run only
-- when that person approves them. args are exactly what runs; status is pending, sending, sent, failed
-- or cancelled.
CREATE TABLE app_actions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id   uuid NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    task_id     text,
    tool        text NOT NULL,
    args        jsonb NOT NULL,
    status      text NOT NULL DEFAULT 'pending',
    result      text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    decided_at  timestamptz
);

ALTER TABLE app_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON app_actions
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
