-- What the workspace's agent remembers between jobs: one short fact per row. A workspace fact is read
-- in all of the agent's work; a project fact when the work can see the project; a person fact only in
-- that person's own assistant, and only they see it. Removed and superseded facts are kept for the
-- record and never read: a removed fact is not added again by the agent on its own.
CREATE TABLE agent_memory (
    org_id        uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id            uuid NOT NULL DEFAULT gen_random_uuid(),
    scope         text NOT NULL CHECK (scope IN ('workspace', 'project', 'person')),
    project_id    uuid,
    member_id     uuid,
    text          text NOT NULL CHECK (length(text) BETWEEN 1 AND 500),
    source_task   text,
    noticed       bool NOT NULL DEFAULT false,  -- the agent noticed it; nobody asked it to remember
    pinned        bool NOT NULL DEFAULT false,  -- read first, so the size cap never leaves it out
    until_date    date,                         -- read through this day, then no more
    superseded_at timestamptz,
    superseded_by uuid,
    removed_at    timestamptz,
    removed_by    uuid,
    created_by    uuid NOT NULL,
    on_behalf_of  uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    tsv           tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
    PRIMARY KEY (org_id, id),
    CHECK ((scope = 'project') = (project_id IS NOT NULL)),
    CHECK ((scope = 'person') = (member_id IS NOT NULL)),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, on_behalf_of) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, removed_by) REFERENCES members(org_id, id)
);
CREATE INDEX agent_memory_live ON agent_memory (org_id, scope) WHERE removed_at IS NULL AND superseded_at IS NULL;
CREATE INDEX agent_memory_tsv ON agent_memory USING gin (tsv);
ALTER TABLE agent_memory ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_memory FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON agent_memory
    USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
    WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
