-- A task has at most one take-on waiting: handing it to the agent twice queues one.
DELETE FROM chat_replies a USING chat_replies b
    WHERE a.kind = 'take_on' AND b.kind = 'take_on' AND a.state = 'pending' AND b.state = 'pending'
    AND a.org_id = b.org_id AND a.task_id = b.task_id AND a.created_at > b.created_at;
CREATE UNIQUE INDEX chat_replies_take_on ON chat_replies (org_id, task_id) WHERE kind = 'take_on' AND state = 'pending';

-- An assistant turn's progress (the writes it made, the pages it found), so a retry on any server or
-- after a restart carries on instead of repeating them. Rows go 15 minutes after the turn's last use.
CREATE TABLE assistant_turns (
    org_id        uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id     uuid NOT NULL,
    turn          text NOT NULL,
    state         jsonb NOT NULL DEFAULT '{}',
    running_until timestamptz NOT NULL,
    lease         text NOT NULL DEFAULT '',
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, member_id, turn),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);
ALTER TABLE assistant_turns ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_turns FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON assistant_turns
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
