-- The background model: an optional, usually cheaper, model for the agent's background jobs (memory
-- upkeep, reviews). Empty uses the main model. helper_credential is sealed like credential; NULL means
-- the main credential, which is only used while both point at the same base URL.
-- monthly_token_cap stops background jobs once this month's tokens (all calls) reach it; NULL is no cap.
ALTER TABLE triage_settings ADD COLUMN helper_base_url text NOT NULL DEFAULT '';
ALTER TABLE triage_settings ADD COLUMN helper_model text NOT NULL DEFAULT '';
ALTER TABLE triage_settings ADD COLUMN helper_credential bytea;
ALTER TABLE triage_settings ADD COLUMN monthly_token_cap bigint CHECK (monthly_token_cap > 0);

-- Background work for the workspace's agent. One pending job of a kind per task: another trigger while
-- one waits bumps triggers and sets its due time, so a burst of triggers is one run, and a trigger that
-- lands while the job runs makes it run again instead of being lost.
CREATE TABLE agent_jobs (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id          uuid NOT NULL DEFAULT gen_random_uuid(),
    kind        text NOT NULL,
    task_id     text,
    member_id   uuid,
    due_at      timestamptz NOT NULL DEFAULT now(),
    state       text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done', 'failed', 'skipped')),
    triggers    int  NOT NULL DEFAULT 1,
    attempts    int  NOT NULL DEFAULT 0,
    claim       uuid,
    claim_until timestamptz,
    last_error  text NOT NULL DEFAULT '',
    result      jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX agent_jobs_one ON agent_jobs (org_id, kind, coalesce(task_id, '')) WHERE state = 'pending';
CREATE INDEX agent_jobs_due ON agent_jobs (org_id, due_at) WHERE state = 'pending';
ALTER TABLE agent_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON agent_jobs
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);

-- What the agent's model calls cost, per workspace and month (UTC), as the endpoint reported it.
-- background separates jobs nobody asked for from triage, chat and the assistant.
CREATE TABLE agent_usage (
    org_id        uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    month         date NOT NULL,
    background    bool NOT NULL,
    input_tokens  bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    calls         bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, month, background)
);
ALTER TABLE agent_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON agent_usage
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
