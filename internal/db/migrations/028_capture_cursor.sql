-- Where memory capture last read a task's chat; NULL = never captured.
ALTER TABLE tasks ADD COLUMN captured_at timestamptz;
