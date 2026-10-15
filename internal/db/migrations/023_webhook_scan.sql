-- The webhook sweep reads only notifications since its last scan, per agent.
ALTER TABLE agent_webhooks ADD COLUMN scanned_at timestamptz;
CREATE INDEX notifications_recent ON notifications (org_id, member_id, created_at);
