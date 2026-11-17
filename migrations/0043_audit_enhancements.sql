BEGIN;

-- Indexes on audit_events for faster role, IP, action, and status queries on admin side
CREATE INDEX IF NOT EXISTS audit_events_role_idx ON audit_events(actor_role, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_ip_idx ON audit_events(ip_address, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_action_idx ON audit_events(action, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_success_idx ON audit_events(success, occurred_at DESC);

COMMIT;
