-- What the agent did while working on a message, in order: what it said it would do, the tools it
-- used, what it looked up. [{type, text, tool, source, query, tasks, error}], shown under its reply.
-- lookups stays for the "Looked up" line.
ALTER TABLE comments ADD COLUMN activity jsonb;

-- agent_done: the workspace's agent finished something a person asked for (a reply, a task taken on,
-- a new task it did). People only; agents never get it, so it never reaches a webhook.
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN ('mention', 'assigned', 'approval', 'due', 'agent_done'));
