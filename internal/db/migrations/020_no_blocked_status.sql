-- "blocked" is no longer a status: a task that waits on another says so with a dependency. Tasks
-- marked blocked become open. ("cancelled" stays as the stored value; people see it as Archived.)
UPDATE tasks SET status = 'open' WHERE status = 'blocked';
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN ('open', 'in_progress', 'done', 'cancelled'));
