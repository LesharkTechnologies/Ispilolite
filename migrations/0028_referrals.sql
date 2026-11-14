BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS referral_code TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS credit_balance NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK(credit_balance >= 0);
CREATE UNIQUE INDEX IF NOT EXISTS users_referral_code_uidx ON users(upper(referral_code)) WHERE referral_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS referrals (
  id UUID PRIMARY KEY,
  referrer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  referred_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE RESTRICT,
  code TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('pending','qualified','cancelled','reversed')),
  reward_amount NUMERIC(14,2) NOT NULL DEFAULT 0,
  qualified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(referrer_id<>referred_id)
);
CREATE INDEX IF NOT EXISTS referrals_referrer_idx ON referrals(referrer_id,status,created_at DESC);

CREATE TABLE IF NOT EXISTS referral_credit_ledger (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  referral_id UUID NOT NULL REFERENCES referrals(id) ON DELETE RESTRICT,
  entry_type TEXT NOT NULL CHECK(entry_type IN ('referrer_reward','referee_reward','spend','reversal')),
  amount NUMERIC(14,2) NOT NULL,
  reference TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(user_id,reference)
);

COMMIT;
