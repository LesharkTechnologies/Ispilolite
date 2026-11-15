BEGIN;

-- Add soft-delete and statutory data retention timestamp columns to users.
-- In accordance with Kenya Data Protection Act 2019 (KDPA) Section 39 & 40,
-- telecommunications/cybersecurity regulations, and financial audit standards,
-- accounts are soft-deleted with a 2-year retention period before permanent anonymization.
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS retention_until TIMESTAMPTZ;

-- Index for monitoring soft-deleted accounts and identifying records due for statutory purge.
CREATE INDEX IF NOT EXISTS users_retention_until_idx ON users (retention_until) WHERE status = 'deleted';

COMMIT;
