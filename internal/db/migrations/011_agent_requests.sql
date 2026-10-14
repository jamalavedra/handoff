-- chat_replies also holds take-on requests: a person handed a task to the workspace's agent, which
-- then does it. Requests get their own id; replies keep one row per triggering comment.
ALTER TABLE chat_replies ADD COLUMN kind text NOT NULL DEFAULT 'reply' CHECK (kind IN ('reply', 'take_on'));
ALTER TABLE chat_replies ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE chat_replies DROP CONSTRAINT chat_replies_pkey;
ALTER TABLE chat_replies ADD PRIMARY KEY (org_id, id);
ALTER TABLE chat_replies ALTER COLUMN comment_id DROP NOT NULL;
CREATE UNIQUE INDEX chat_replies_comment ON chat_replies (org_id, comment_id) WHERE comment_id IS NOT NULL;
