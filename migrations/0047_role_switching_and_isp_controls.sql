BEGIN;

-- Add role_changed_at to users to track the 24-hour rate limit on account type changes
ALTER TABLE users ADD COLUMN IF NOT EXISTS role_changed_at TIMESTAMPTZ;

-- Allow 'both' as a valid role when a user is both an ISP and Technician
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('customer', 'client', 'isp', 'technician', 'admin', 'both'));

-- Add secondary_role column to users for multi-role capability tracking
ALTER TABLE users ADD COLUMN IF NOT EXISTS secondary_role TEXT NOT NULL DEFAULT '';

COMMIT;
