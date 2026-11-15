BEGIN;

CREATE TABLE IF NOT EXISTS user_favorites (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL CHECK(target_type IN ('isp','technician')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,target_type,target_id),
  CHECK(user_id<>target_id)
);
CREATE INDEX IF NOT EXISTS user_favorites_target_idx ON user_favorites(target_type,target_id,created_at DESC);

CREATE TABLE IF NOT EXISTS user_reactions (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL CHECK(target_type IN ('user_profile','technician_post','review')),
  target_id UUID NOT NULL,
  reaction_type TEXT NOT NULL CHECK(reaction_type IN ('like','helpful')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,target_type,target_id,reaction_type)
);
CREATE INDEX IF NOT EXISTS user_reactions_target_idx ON user_reactions(target_type,target_id,reaction_type);

CREATE TABLE IF NOT EXISTS user_device_tokens (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform TEXT NOT NULL CHECK(platform IN ('android','ios','web')),
  token TEXT NOT NULL UNIQUE,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS user_device_tokens_active_idx ON user_device_tokens(user_id) WHERE enabled=true;

-- Durable hand-off for a separately approved payout/refund rail adapter. This
-- table does not claim money moved; only a signed settlement callback may mark
-- an item completed and update the escrow ledger.
CREATE TABLE IF NOT EXISTS payment_settlement_outbox (
  id UUID PRIMARY KEY,
  escrow_id UUID NOT NULL REFERENCES escrows(id) ON DELETE CASCADE,
  milestone_id UUID REFERENCES escrow_milestones(id) ON DELETE CASCADE,
  action TEXT NOT NULL CHECK(action IN ('release','refund')),
  amount NUMERIC(14,2) NOT NULL CHECK(amount>0),
  currency TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','completed','failed')),
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS payment_settlement_outbox_open_uidx
  ON payment_settlement_outbox(escrow_id,COALESCE(milestone_id,'00000000-0000-0000-0000-000000000000'::uuid),action)
  WHERE status IN ('pending','processing');

COMMIT;
