BEGIN;

CREATE TABLE IF NOT EXISTS job_pairing_sessions (
  id UUID PRIMARY KEY,
  job_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  customer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  amount NUMERIC(14,2) NOT NULL DEFAULT 0.00,
  currency TEXT NOT NULL DEFAULT 'KES',
  nonce TEXT NOT NULL UNIQUE,
  verification_code TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL,
  qr_payload TEXT NOT NULL,
  deep_link TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','confirmed','expired','cancelled')),
  expires_at TIMESTAMPTZ NOT NULL,
  confirmed_at TIMESTAMPTZ,
  confirmed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  notes TEXT NOT NULL DEFAULT '',
  rating INTEGER CHECK(rating IS NULL OR (rating >= 1 AND rating <= 5)),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS job_pairing_sessions_job_idx ON job_pairing_sessions(job_id, status);
CREATE INDEX IF NOT EXISTS job_pairing_sessions_nonce_idx ON job_pairing_sessions(nonce);
CREATE INDEX IF NOT EXISTS job_pairing_sessions_code_idx ON job_pairing_sessions(job_id, verification_code);

COMMIT;
