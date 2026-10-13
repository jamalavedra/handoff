-- notifications: @mentions, assignments and approval requests addressed to one member.
CREATE TABLE notifications (
    id         bigserial PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id  uuid NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('mention', 'assigned', 'approval')),
    task_id    text NOT NULL,
    actor_id   uuid NOT NULL,
    excerpt    text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at    timestamptz,
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, actor_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);
CREATE INDEX notifications_member ON notifications (org_id, member_id, id DESC);

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON notifications
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
