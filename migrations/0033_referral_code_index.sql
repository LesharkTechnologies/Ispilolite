BEGIN;

DROP INDEX IF EXISTS users_referral_code_uidx;
CREATE UNIQUE INDEX IF NOT EXISTS users_referral_code_uidx
  ON users (upper(referral_code))
  WHERE referral_code IS NOT NULL AND referral_code <> '';

COMMIT;
