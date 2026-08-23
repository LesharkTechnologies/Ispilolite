BEGIN;

ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS requester_role TEXT NOT NULL DEFAULT 'customer';
ALTER TABLE service_requests DROP CONSTRAINT IF EXISTS service_requests_requester_role_chk;
ALTER TABLE service_requests ADD CONSTRAINT service_requests_requester_role_chk CHECK (requester_role IN ('customer','isp','technician','admin'));
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS terminated_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS termination_reason TEXT NOT NULL DEFAULT '';

COMMIT;
