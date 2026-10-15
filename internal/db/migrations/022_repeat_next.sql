-- next_id is the task created when this repeating one was closed, so closing it again after a reopen
-- doesn't start a second one.
ALTER TABLE tasks ADD COLUMN next_id text;
-- repeat_day is the series' day of the month, so Oct 31 -> Nov 30 -> Dec 31 instead of drifting to the 30th.
ALTER TABLE tasks ADD COLUMN repeat_day smallint;
