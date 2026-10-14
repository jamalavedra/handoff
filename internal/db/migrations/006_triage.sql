-- triage_settings: the agent that evaluates each new task a person creates, and the model it uses.
-- credential is sealed with HANDOFF_SECRET (AES-GCM); it is never returned to clients.
CREATE TABLE triage_settings (
    org_id     uuid PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
    agent_id   uuid NOT NULL,
    base_url   text NOT NULL,
    model      text NOT NULL,
    credential bytea NOT NULL,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, agent_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

ALTER TABLE triage_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE triage_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON triage_settings
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);

-- triage: NULL (never triaged), pending, done or failed.
-- triage_explicit: fields the creator set on purpose, which triage leaves alone.
ALTER TABLE tasks ADD COLUMN triage text CHECK (triage IN ('pending', 'done', 'failed'));
ALTER TABLE tasks ADD COLUMN triage_explicit text[] NOT NULL DEFAULT '{}';
ALTER TABLE tasks ADD COLUMN triage_error text NOT NULL DEFAULT '';
-- triage_claim: the worker evaluating it right now (any server); the lease lets another take over after a crash.
ALTER TABLE tasks ADD COLUMN triage_claim uuid;
ALTER TABLE tasks ADD COLUMN triage_claim_until timestamptz;
CREATE INDEX tasks_triage_pending ON tasks (org_id) WHERE triage = 'pending';
