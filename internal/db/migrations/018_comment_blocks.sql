-- Rich pieces the agent attaches to its message (images, a map): [{type, data}], rendered by type.
ALTER TABLE comments ADD COLUMN blocks jsonb;
