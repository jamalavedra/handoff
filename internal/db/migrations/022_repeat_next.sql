-- next_id is the task created when this repeating one was closed, so closing it again after a reopen
-- doesn't start a second one.
ALTER TABLE tasks ADD COLUMN next_id text;
