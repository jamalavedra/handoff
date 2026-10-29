-- app_codes: the short code a signed-in person types into the mobile app, which redeems it for a session.
CREATE TABLE app_codes (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id  uuid NOT NULL,
    code_hash  bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);
CREATE INDEX app_codes_member ON app_codes (org_id, member_id);

ALTER TABLE app_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON app_codes
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
