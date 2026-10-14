-- chat_replies: a person asked the workspace's agent something in a task's chat. Stored with the
-- comment so it survives restarts; a worker claims it (lease) and answers once.
CREATE TABLE chat_replies (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    comment_id  uuid NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    task_id     text NOT NULL,
    author_id   uuid NOT NULL,
    state       text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done')),
    claim_until timestamptz,
    attempts    int NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, comment_id),
    FOREIGN KEY (org_id, author_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);
CREATE INDEX chat_replies_pending ON chat_replies (org_id, created_at) WHERE state = 'pending';

ALTER TABLE chat_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_replies FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON chat_replies
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
