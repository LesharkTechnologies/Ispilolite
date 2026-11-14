BEGIN;

ALTER TABLE isp_packages ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT 'basic';
ALTER TABLE isp_packages ADD COLUMN IF NOT EXISTS overage_price NUMERIC(14,4) NOT NULL DEFAULT 0 CHECK(overage_price >= 0);
ALTER TABLE isp_package_versions ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT 'basic';
ALTER TABLE isp_package_versions ADD COLUMN IF NOT EXISTS overage_price NUMERIC(14,4) NOT NULL DEFAULT 0 CHECK(overage_price >= 0);

ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT 'basic';
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS billing_cycle TEXT NOT NULL DEFAULT 'monthly';
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS auto_renew BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS included_usage NUMERIC(14,3) NOT NULL DEFAULT 0;
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS usage_value NUMERIC(14,3) NOT NULL DEFAULT 0;
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS overage_price NUMERIC(14,4) NOT NULL DEFAULT 0;
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS overage_amount NUMERIC(14,2) NOT NULL DEFAULT 0;
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS current_period_start TIMESTAMPTZ;
ALTER TABLE package_subscriptions ADD COLUMN IF NOT EXISTS current_period_end TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS package_usage_events (
  id UUID PRIMARY KEY,
  subscription_id UUID NOT NULL REFERENCES package_subscriptions(id) ON DELETE CASCADE,
  isp_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  usage_value NUMERIC(14,3) NOT NULL CHECK(usage_value > 0),
  external_ref TEXT NOT NULL,
  measured_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(subscription_id,external_ref)
);
CREATE INDEX IF NOT EXISTS package_usage_events_subscription_idx
  ON package_usage_events(subscription_id,measured_at DESC);

COMMIT;
