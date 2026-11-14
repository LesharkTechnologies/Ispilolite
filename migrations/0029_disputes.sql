BEGIN;

CREATE TABLE IF NOT EXISTS disputes (
  id UUID PRIMARY KEY,
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE RESTRICT,
  escrow_id UUID REFERENCES escrows(id) ON DELETE RESTRICT,
  opened_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  reason TEXT NOT NULL,
  description TEXT NOT NULL,
  evidence_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL CHECK(status IN ('open','under_review','resolved','rejected','cancelled')),
  resolution TEXT NOT NULL DEFAULT '',
  resolution_amount NUMERIC(14,2) NOT NULL DEFAULT 0,
  resolution_note TEXT NOT NULL DEFAULT '',
  resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS disputes_job_active_uidx ON disputes(request_id)
  WHERE status IN ('open','under_review');
CREATE INDEX IF NOT EXISTS disputes_admin_idx ON disputes(status,created_at);

CREATE TABLE IF NOT EXISTS dispute_comments (
  id UUID PRIMARY KEY,
  dispute_id UUID NOT NULL REFERENCES disputes(id) ON DELETE CASCADE,
  author_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  body TEXT NOT NULL DEFAULT '',
  evidence_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(body<>'' OR jsonb_array_length(evidence_urls)>0)
);

COMMIT;
