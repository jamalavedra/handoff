-- The workspace's agent can hand a task back to the person it works for when it needs their input:
-- waiting_on is who it waits on. Reassigning or closing the task ends the wait. options are the
-- quick answers the agent offered with its question.
ALTER TABLE tasks ADD COLUMN waiting_on uuid;
ALTER TABLE tasks ADD CONSTRAINT tasks_waiting_on_fkey FOREIGN KEY (org_id, waiting_on) REFERENCES members(org_id, id);
ALTER TABLE comments ADD COLUMN options jsonb;

CREATE FUNCTION tasks_end_wait() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.waiting_on IS NOT NULL AND NEW.waiting_on IS NOT DISTINCT FROM OLD.waiting_on
        AND (NEW.assignee_id IS DISTINCT FROM OLD.assignee_id OR NEW.status IN ('done', 'cancelled') OR NEW.deleted_at IS NOT NULL) THEN
        NEW.waiting_on := NULL;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER tasks_end_wait BEFORE UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION tasks_end_wait();
