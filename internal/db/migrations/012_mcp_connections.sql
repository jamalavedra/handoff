-- mcp_connections: remote MCP servers the workspace's agent may use (web search, docs, trackers).
-- secret is sealed with HANDOFF_SECRET; tools caches what the server offers, enabled what's switched on.
CREATE TABLE mcp_connections (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id          uuid NOT NULL DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    url         text NOT NULL,
    auth        text NOT NULL DEFAULT 'none' CHECK (auth IN ('none', 'bearer', 'header')),
    header_name text NOT NULL DEFAULT '',
    secret      bytea,
    tools       jsonb NOT NULL DEFAULT '[]',
    enabled     text[] NOT NULL DEFAULT '{}',
    last_error  text NOT NULL DEFAULT '',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    UNIQUE (org_id, name)
);

ALTER TABLE mcp_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_connections FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON mcp_connections
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
