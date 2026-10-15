-- A comment can reply to another one in the same chat, optionally quoting the part it's about.
-- Deleting the original keeps the reply (and its quote); it just no longer points anywhere.
ALTER TABLE comments ADD CONSTRAINT comments_org_id_id_key UNIQUE (org_id, id);
ALTER TABLE comments ADD COLUMN reply_to uuid;
ALTER TABLE comments ADD COLUMN quote text CHECK (char_length(quote) <= 500);
ALTER TABLE comments ADD CONSTRAINT comments_reply_to_fkey FOREIGN KEY (org_id, reply_to)
    REFERENCES comments(org_id, id) ON DELETE SET NULL (reply_to);
