CREATE TABLE orgs (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                 text NOT NULL UNIQUE,
    name                 text NOT NULL,
    id_prefix            text NOT NULL,
    custom_domain        text UNIQUE,
    allowed_email_domain text,
    created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE members (
    id              uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN ('human', 'agent')),
    name            text NOT NULL,
    email           text,
    role            text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    status          text NOT NULL DEFAULT 'active' CHECK (status IN ('invited', 'active', 'paused', 'revoked')),
    owner_id        uuid,
    oauth_client_id text,
    permissions     jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, owner_id) REFERENCES members(org_id, id)
);
CREATE UNIQUE INDEX members_email ON members (org_id, lower(email)) WHERE kind = 'human';

CREATE TABLE projects (
    id          uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    visibility  text NOT NULL DEFAULT 'company' CHECK (visibility IN ('company', 'private')),
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, created_by) REFERENCES members(org_id, id)
);
CREATE UNIQUE INDEX projects_name ON projects (org_id, lower(name));

CREATE TABLE project_members (
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id uuid NOT NULL,
    member_id  uuid NOT NULL,
    PRIMARY KEY (org_id, project_id, member_id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

CREATE TABLE tasks (
    org_id           uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id               text NOT NULL,
    project_id       uuid NOT NULL,
    title            text NOT NULL,
    body             text NOT NULL DEFAULT '',
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'blocked', 'done', 'cancelled')),
    priority         int  NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    assignee_id      uuid,
    due_at           timestamptz,
    labels           text[] NOT NULL DEFAULT '{}',
    parent_id        text,
    external_id      text,
    claimed_by       uuid,
    claim_expires_at timestamptz,
    created_by       uuid NOT NULL,
    on_behalf_of     uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    closed_at        timestamptz,
    close_reason     text NOT NULL DEFAULT '',
    deleted_at       timestamptz,
    deleted_by       uuid,
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id),
    FOREIGN KEY (org_id, assignee_id) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, claimed_by) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, created_by) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, on_behalf_of) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, deleted_by) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, parent_id) REFERENCES tasks(org_id, id) ON DELETE SET NULL (parent_id)
);
CREATE UNIQUE INDEX tasks_external_id ON tasks (org_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX tasks_project ON tasks (org_id, project_id) WHERE deleted_at IS NULL;
CREATE INDEX tasks_assignee ON tasks (org_id, assignee_id) WHERE deleted_at IS NULL;

CREATE TABLE task_deps (
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    blocker_id text NOT NULL,
    blocked_id text NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id),
    FOREIGN KEY (org_id, blocker_id) REFERENCES tasks(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, blocked_id) REFERENCES tasks(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES members(org_id, id)
);
CREATE INDEX task_deps_blocked ON task_deps (org_id, blocked_id);

CREATE TABLE comments (
    id           uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    task_id      text NOT NULL,
    author_id    uuid NOT NULL,
    on_behalf_of uuid,
    body         text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, task_id) REFERENCES tasks(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, author_id) REFERENCES members(org_id, id)
);
CREATE INDEX comments_task ON comments (org_id, task_id);

-- Append-only audit log. task_id has no FK so history outlives purged tasks.
CREATE TABLE events (
    id           bigserial PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES orgs(id),
    task_id      text,
    actor_id     uuid NOT NULL,
    on_behalf_of uuid,
    via          text NOT NULL,
    kind         text NOT NULL,
    data         jsonb NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, actor_id) REFERENCES members(org_id, id)
);
CREATE INDEX events_task ON events (org_id, task_id, id);
CREATE INDEX events_actor ON events (org_id, actor_id, id);

CREATE FUNCTION events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'events are append-only';
END $$;
CREATE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON events
    FOR EACH ROW EXECUTE FUNCTION events_append_only();

CREATE TABLE approvals (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    agent_id   uuid NOT NULL,
    action     text NOT NULL,
    task_id    text NOT NULL,
    payload    jsonb NOT NULL DEFAULT '{}',
    status     text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    decided_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    FOREIGN KEY (org_id, agent_id) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, decided_by) REFERENCES members(org_id, id)
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id  uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

CREATE TABLE login_tokens (
    token_hash bytea PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email      text NOT NULL,
    name       text NOT NULL DEFAULT '',
    next_path  text NOT NULL DEFAULT '/',
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE TABLE api_keys (
    id           uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id    uuid NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    hint         text NOT NULL,
    created_by   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz,
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES members(org_id, id)
);

CREATE TABLE oauth_clients (
    org_id        uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    client_id     text NOT NULL,
    secret_hash   bytea,
    name          text NOT NULL,
    redirect_uris text[] NOT NULL,
    auth_method   text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, client_id)
);

CREATE TABLE oauth_requests (
    id             uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    client_id      text NOT NULL,
    redirect_uri   text NOT NULL,
    state          text NOT NULL DEFAULT '',
    code_challenge text NOT NULL,
    scope          text NOT NULL DEFAULT '',
    resource       text NOT NULL DEFAULT '',
    expires_at     timestamptz NOT NULL,
    FOREIGN KEY (org_id, client_id) REFERENCES oauth_clients(org_id, client_id) ON DELETE CASCADE
);

CREATE TABLE oauth_codes (
    code_hash      bytea PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    client_id      text NOT NULL,
    member_id      uuid NOT NULL,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz,
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

CREATE TABLE oauth_tokens (
    token_hash bytea PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('access', 'refresh')),
    client_id  text NOT NULL,
    member_id  uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

-- Every org-owned table is isolated by app.org_id, which the server sets per
-- transaction. With app.org_id unset no rows are visible or writable.
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['members', 'projects', 'project_members', 'tasks', 'task_deps', 'comments',
        'events', 'approvals', 'sessions', 'login_tokens', 'api_keys', 'oauth_clients', 'oauth_requests',
        'oauth_codes', 'oauth_tokens']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY org_isolation ON %I
            USING (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)
            WITH CHECK (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)', t);
    END LOOP;
END $$;
