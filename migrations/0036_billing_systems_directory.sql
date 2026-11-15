BEGIN;

CREATE TABLE IF NOT EXISTS billing_systems (
  id UUID PRIMARY KEY,
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  logo_url TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  owner_company TEXT NOT NULL DEFAULT '',
  owner_phone TEXT NOT NULL DEFAULT '',
  support_phone TEXT NOT NULL DEFAULT '',
  support_whatsapp TEXT NOT NULL DEFAULT '',
  support_email TEXT NOT NULL DEFAULT '',
  support_hours TEXT NOT NULL DEFAULT '',
  website_url TEXT NOT NULL DEFAULT '',
  login_url TEXT NOT NULL DEFAULT '',
  referral_url TEXT NOT NULL DEFAULT '',
  pricing_updated_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','archived','suspended')),
  domain_verified BOOLEAN NOT NULL DEFAULT false,
  business_verified BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS billing_systems_status_idx ON billing_systems(status,name);

CREATE TABLE IF NOT EXISTS billing_system_managers (
  billing_system_id UUID NOT NULL REFERENCES billing_systems(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK(role IN ('owner','manager')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(billing_system_id,user_id)
);

CREATE TABLE IF NOT EXISTS billing_system_pricing (
  id UUID PRIMARY KEY,
  billing_system_id UUID NOT NULL REFERENCES billing_systems(id) ON DELETE CASCADE,
  service_type TEXT NOT NULL CHECK(service_type IN ('hotspot','pppoe','static_ip','combined')),
  currency TEXT NOT NULL DEFAULT 'KES',
  billing_period TEXT NOT NULL DEFAULT 'monthly',
  amount NUMERIC(14,2),
  setup_fee NUMERIC(14,2) NOT NULL DEFAULT 0,
  subscriber_limit INTEGER,
  extra_subscriber_cost NUMERIC(14,2),
  transaction_fee_percent NUMERIC(6,3),
  notes TEXT NOT NULL DEFAULT '',
  available BOOLEAN NOT NULL DEFAULT true,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(billing_system_id,service_type)
);

CREATE TABLE IF NOT EXISTS billing_system_reviews (
  id UUID PRIMARY KEY,
  billing_system_id UUID NOT NULL REFERENCES billing_systems(id) ON DELETE CASCADE,
  reviewer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reliability INTEGER NOT NULL CHECK(reliability BETWEEN 1 AND 5),
  ease_of_use INTEGER NOT NULL CHECK(ease_of_use BETWEEN 1 AND 5),
  support INTEGER NOT NULL CHECK(support BETWEEN 1 AND 5),
  features INTEGER NOT NULL CHECK(features BETWEEN 1 AND 5),
  value_for_money INTEGER NOT NULL CHECK(value_for_money BETWEEN 1 AND 5),
  comment TEXT NOT NULL DEFAULT '',
  use_verified BOOLEAN NOT NULL DEFAULT false,
  owner_reply TEXT NOT NULL DEFAULT '',
  owner_replied_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(billing_system_id,reviewer_id)
);
CREATE INDEX IF NOT EXISTS billing_system_reviews_idx ON billing_system_reviews(billing_system_id,created_at DESC);

CREATE TABLE IF NOT EXISTS billing_system_flags (
  id UUID PRIMARY KEY,
  billing_system_id UUID NOT NULL REFERENCES billing_systems(id) ON DELETE CASCADE,
  reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','resolved','dismissed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(billing_system_id,reporter_id)
);

CREATE TABLE IF NOT EXISTS billing_system_claims (
  id UUID PRIMARY KEY,
  billing_system_id UUID NOT NULL REFERENCES billing_systems(id) ON DELETE CASCADE,
  claimant_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  evidence TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  UNIQUE(billing_system_id,claimant_id)
);

COMMIT;