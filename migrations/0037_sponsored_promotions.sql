BEGIN;

-- Sponsored listings for billing systems, ISPs and technician profiles.
-- A promotion snapshots the listing name so archived/renamed listings still
-- have readable history. listing_id is polymorphic:
--   billing_system -> billing_systems.id
--   isp            -> users.id (the ISP account, which is also isps.id)
--   technician     -> users.id (resolved through technicians.user_id)
CREATE TABLE IF NOT EXISTS promotions (
  id UUID PRIMARY KEY,
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  listing_type TEXT NOT NULL CHECK(listing_type IN ('billing_system','isp','technician')),
  listing_id UUID NOT NULL,
  listing_name TEXT NOT NULL,
  destination_type TEXT NOT NULL DEFAULT 'internal' CHECK(destination_type IN ('internal','external')),
  destination_url TEXT NOT NULL,
  placement TEXT NOT NULL DEFAULT 'directory',
  status TEXT NOT NULL DEFAULT 'pending_payment' CHECK(status IN ('pending_payment','scheduled','active','expired','cancelled','suspended')),
  starts_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  amount NUMERIC(14,2) NOT NULL CHECK(amount >= 0),
  currency TEXT NOT NULL DEFAULT 'KES',
  renewed_from_id UUID REFERENCES promotions(id) ON DELETE SET NULL,
  cancelled_at TIMESTAMPTZ,
  suspended_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(expires_at > starts_at)
);
CREATE INDEX IF NOT EXISTS promotions_owner_idx ON promotions(owner_id,created_at DESC);
CREATE INDEX IF NOT EXISTS promotions_listing_idx ON promotions(listing_type,listing_id,created_at DESC);
CREATE INDEX IF NOT EXISTS promotions_live_idx ON promotions(status,starts_at,expires_at,placement);

CREATE TABLE IF NOT EXISTS promotion_payments (
  id UUID PRIMARY KEY,
  promotion_id UUID NOT NULL REFERENCES promotions(id) ON DELETE CASCADE,
  amount NUMERIC(14,2) NOT NULL CHECK(amount >= 0),
  currency TEXT NOT NULL DEFAULT 'KES',
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','paid','failed','refunded','waived')),
  provider TEXT NOT NULL DEFAULT '',
  provider_reference TEXT NOT NULL DEFAULT '',
  receipt TEXT NOT NULL DEFAULT '',
  confirmed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  paid_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS promotion_payments_promotion_idx ON promotion_payments(promotion_id,created_at DESC);
CREATE INDEX IF NOT EXISTS promotion_payments_pending_idx ON promotion_payments(status,created_at);

CREATE TABLE IF NOT EXISTS promotion_events (
  id BIGSERIAL PRIMARY KEY,
  promotion_id UUID NOT NULL REFERENCES promotions(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL CHECK(event_type IN ('impression','click')),
  viewer_key TEXT NOT NULL,
  viewer_role TEXT NOT NULL DEFAULT 'visitor' CHECK(viewer_role IN ('customer','technician','isp','visitor')),
  viewer_county TEXT NOT NULL DEFAULT '',
  placement TEXT NOT NULL DEFAULT '',
  dedup_bucket BIGINT NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One counted impression per viewer/placement per 5 minutes and one counted
-- click per viewer/placement per minute. This filters reload loops and
-- repeated artificial clicks while still preserving genuine traffic patterns.
CREATE UNIQUE INDEX IF NOT EXISTS promotion_events_dedup_uidx
  ON promotion_events(promotion_id,event_type,viewer_key,placement,dedup_bucket);
CREATE INDEX IF NOT EXISTS promotion_events_promotion_time_idx
  ON promotion_events(promotion_id,occurred_at DESC);
CREATE INDEX IF NOT EXISTS promotion_events_promotion_type_time_idx
  ON promotion_events(promotion_id,event_type,occurred_at DESC);

CREATE TABLE IF NOT EXISTS promotion_enquiries (
  id UUID PRIMARY KEY,
  promotion_id UUID NOT NULL REFERENCES promotions(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  phone TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  consent BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL DEFAULT 'new' CHECK(status IN ('new','read','responded','closed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS promotion_enquiries_promotion_idx ON promotion_enquiries(promotion_id,created_at DESC);
CREATE INDEX IF NOT EXISTS promotion_enquiries_user_time_idx ON promotion_enquiries(user_id,created_at DESC);

COMMIT;
