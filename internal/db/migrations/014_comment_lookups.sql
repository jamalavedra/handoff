-- What the agent looked up in connected services (web search, internal tools…) before writing a
-- comment: [{source, tool, query, failed}], shown as a line under its message.
ALTER TABLE comments ADD COLUMN lookups jsonb;
