-- The webhook sweep reads only recent notifications per agent.
CREATE INDEX notifications_recent ON notifications (org_id, member_id, created_at);
