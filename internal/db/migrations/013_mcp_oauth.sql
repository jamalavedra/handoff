-- Connections can sign in with the service (OAuth 2.1, as MCP servers do): the tokens are sealed in
-- secret and refreshed when they expire. mcp_oauth_pending holds a sign-in between start and callback.
ALTER TABLE mcp_connections DROP CONSTRAINT mcp_connections_auth_check;
ALTER TABLE mcp_connections ADD CONSTRAINT mcp_connections_auth_check CHECK (auth IN ('none', 'bearer', 'header', 'oauth'));

CREATE TABLE mcp_oauth_pending (
    org_id         uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    state          text NOT NULL,
    name           text NOT NULL,
    url            text NOT NULL,
    redirect_uri   text NOT NULL,
    client_id      text NOT NULL,
    verifier       text NOT NULL,
    token_endpoint text NOT NULL,
    created_by     uuid NOT NULL,
    expires_at     timestamptz NOT NULL,
    PRIMARY KEY (org_id, state)
);

ALTER TABLE mcp_oauth_pending ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_oauth_pending FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON mcp_oauth_pending
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
