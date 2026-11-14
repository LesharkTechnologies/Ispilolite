BEGIN;

ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS warranty_days INTEGER NOT NULL DEFAULT 0 CHECK(warranty_days BETWEEN 0 AND 3650);
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS warranty_expires_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS warranty_claims (
  id UUID PRIMARY KEY,
  original_request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE RESTRICT,
  replacement_request_id UUID REFERENCES service_requests(id) ON DELETE SET NULL,
  customer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  description TEXT NOT NULL,
  photo_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL CHECK(status IN ('open','rematched','in_progress','resolved','rejected','cancelled')),
  resolution_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS warranty_claims_customer_idx ON warranty_claims(customer_id,status,created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS warranty_claims_active_uidx ON warranty_claims(original_request_id,customer_id)
  WHERE status IN ('open','rematched','in_progress');

COMMIT;
