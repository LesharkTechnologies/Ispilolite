BEGIN;

-- Promotions now require an explicit admin content approval before they can be
-- served to customers. Payment remains a second gate: an approved promotion
-- with an unpaid invoice stays in pending_payment and is not public.
ALTER TABLE promotions DROP CONSTRAINT IF EXISTS promotions_status_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_status_check
  CHECK(status IN ('pending_approval','pending_payment','scheduled','active','expired','cancelled','suspended','rejected'));
ALTER TABLE promotions ALTER COLUMN status SET DEFAULT 'pending_approval';

ALTER TABLE promotions ADD COLUMN IF NOT EXISTS approved_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS rejected_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS rejected_at TIMESTAMPTZ;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS rejection_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS promotions_review_idx
  ON promotions(status,created_at)
  WHERE status IN ('pending_approval','pending_payment');

COMMIT;
