ALTER TABLE members ADD COLUMN seen_baseline timestamptz NOT NULL DEFAULT now();
ALTER TABLE members ADD COLUMN feed_cursor bigint NOT NULL DEFAULT 0;
CREATE INDEX events_org ON events (org_id, id);

-- task_seen: when each member last opened a task, for "changed by someone else" markers.
CREATE TABLE task_seen (
    org_id    uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    member_id uuid NOT NULL,
    task_id   text NOT NULL,
    seen_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, member_id, task_id),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

-- presence: who has a task open right now, and who is typing a comment.
CREATE TABLE presence (
    org_id       uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    task_id      text NOT NULL,
    member_id    uuid NOT NULL,
    here_until   timestamptz NOT NULL,
    typing_until timestamptz NOT NULL DEFAULT '-infinity',
    PRIMARY KEY (org_id, task_id, member_id),
    FOREIGN KEY (org_id, member_id) REFERENCES members(org_id, id) ON DELETE CASCADE
);

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['task_seen', 'presence']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY org_isolation ON %I
            USING (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)
            WITH CHECK (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)', t);
    END LOOP;
END $$;

-- handoff_live carries {org, kind, task, actor, project} to open screens; delivered on commit.
CREATE FUNCTION notify_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('handoff_live', json_build_object('org', NEW.org_id, 'kind', 'event', 'task', NEW.task_id,
        'actor', NEW.actor_id, 'project', (SELECT project_id FROM tasks WHERE org_id = NEW.org_id AND id = NEW.task_id))::text);
    RETURN NULL;
END $$;
CREATE TRIGGER events_notify AFTER INSERT ON events FOR EACH ROW EXECUTE FUNCTION notify_event();

CREATE FUNCTION notify_presence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (OLD.here_until > now()) = (NEW.here_until > now())
        AND (OLD.typing_until > now()) = (NEW.typing_until > now()) THEN
        RETURN NULL;
    END IF;
    PERFORM pg_notify('handoff_live', json_build_object('org', NEW.org_id, 'kind', 'presence', 'task', NEW.task_id,
        'actor', NEW.member_id, 'project', (SELECT project_id FROM tasks WHERE org_id = NEW.org_id AND id = NEW.task_id))::text);
    RETURN NULL;
END $$;
CREATE TRIGGER presence_notify AFTER INSERT OR UPDATE ON presence FOR EACH ROW EXECUTE FUNCTION notify_presence();
