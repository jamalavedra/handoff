-- app_sessions: each person's Composio session for Settings → Apps (their own Gmail, Calendar…).
-- connected is which apps were connected when tools was last listed from the session's MCP server;
-- tools caches the read-only ones the person's assistant may use.
CREATE TABLE app_sessions (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id   uuid NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    session_id  text NOT NULL,
    mcp_url     text NOT NULL,
    toolkits    text[] NOT NULL DEFAULT '{}',
    connected   text[] NOT NULL DEFAULT '{}',
    tools       jsonb NOT NULL DEFAULT '[]',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, member_id)
);

ALTER TABLE app_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON app_sessions
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
