-- xid orders the what_changed feed by transaction. Event ids are handed out before commit, so a slow
-- transaction can commit an id below a cursor that already moved on; transactions below the snapshot xmin can't.
ALTER TABLE events ADD COLUMN xid xid8 NOT NULL DEFAULT pg_current_xact_id();
CREATE INDEX events_xid ON events (org_id, xid, id);

-- from_project lets viewers of a task's old project hear that it moved away.
CREATE OR REPLACE FUNCTION notify_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('handoff_live', json_build_object('org', NEW.org_id, 'kind', 'event', 'task', NEW.task_id,
        'actor', NEW.actor_id, 'project', (SELECT project_id FROM tasks WHERE org_id = NEW.org_id AND id = NEW.task_id),
        'from_project', NEW.data->>'from_project_id')::text);
    RETURN NULL;
END $$;

-- what_changed cursors are now "xid:id"; the old event-id cursors can't be translated.
ALTER TABLE members DROP COLUMN feed_cursor;
ALTER TABLE members ADD COLUMN feed_after text NOT NULL DEFAULT '';
