BEGIN;

CREATE TABLE IF NOT EXISTS payment_intents (
  id UUID PRIMARY KEY,
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE RESTRICT,
  payer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  payee_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  provider TEXT NOT NULL CHECK(provider IN ('mpesa','airtel')),
  external_id TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'KES',
  amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
  status TEXT NOT NULL CHECK(status IN ('created','pending','succeeded','failed','cancelled')),
  idempotency_key TEXT NOT NULL,
  failure_reason TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(payer_id,idempotency_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS payment_intents_provider_external_uidx
  ON payment_intents(provider,external_id) WHERE external_id<>'';

CREATE TABLE IF NOT EXISTS escrows (
  id UUID PRIMARY KEY,
  payment_id UUID NOT NULL UNIQUE REFERENCES payment_intents(id) ON DELETE RESTRICT,
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE RESTRICT,
  payer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  payee_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  currency TEXT NOT NULL,
  amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
  status TEXT NOT NULL CHECK(status IN ('held','release_pending','released','refund_pending','refunded','disputed')),
  held_at TIMESTAMPTZ,
  released_at TIMESTAMPTZ,
  refunded_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS escrows_request_idx ON escrows(request_id,status);

CREATE TABLE IF NOT EXISTS escrow_milestones (
  id UUID PRIMARY KEY,
  payment_id UUID NOT NULL REFERENCES payment_intents(id) ON DELETE CASCADE,
  escrow_id UUID REFERENCES escrows(id) ON DELETE RESTRICT,
  title TEXT NOT NULL,
  amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
  sequence INTEGER NOT NULL CHECK(sequence > 0),
  status TEXT NOT NULL CHECK(status IN ('pending','funded','release_pending','released','refund_pending','refunded')),
  approved_at TIMESTAMPTZ,
  released_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(payment_id,sequence)
);

CREATE TABLE IF NOT EXISTS payment_webhook_events (
  id UUID PRIMARY KEY,
  provider TEXT NOT NULL,
  external_event_id TEXT NOT NULL,
  external_payment_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider,external_event_id)
);

CREATE TABLE IF NOT EXISTS payment_ledger (
  id UUID PRIMARY KEY,
  escrow_id UUID NOT NULL REFERENCES escrows(id) ON DELETE RESTRICT,
  milestone_id UUID REFERENCES escrow_milestones(id) ON DELETE RESTRICT,
  entry_type TEXT NOT NULL CHECK(entry_type IN ('hold','release_requested','released','refund_requested','refunded')),
  amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
  currency TEXT NOT NULL,
  reference TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS payment_ledger_escrow_idx ON payment_ledger(escrow_id,created_at);

COMMIT;
