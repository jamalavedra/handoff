-- repeat: completing the task creates the next occurrence, due one interval later.
ALTER TABLE tasks ADD COLUMN repeat text NOT NULL DEFAULT ''
    CHECK (repeat IN ('', 'daily', 'weekdays', 'weekly', 'biweekly', 'monthly', 'yearly'));
