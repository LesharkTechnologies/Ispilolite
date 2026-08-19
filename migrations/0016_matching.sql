BEGIN;

CREATE TABLE IF NOT EXISTS matching_weights (
  id BOOLEAN PRIMARY KEY DEFAULT true,
  distance_weight NUMERIC(8,5) NOT NULL DEFAULT .35,
  rating_weight NUMERIC(8,5) NOT NULL DEFAULT .30,
  activity_weight NUMERIC(8,5) NOT NULL DEFAULT .20,
  coverage_weight NUMERIC(8,5) NOT NULL DEFAULT .10,
  availability_weight NUMERIC(8,5) NOT NULL DEFAULT .05,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO matching_weights(id) VALUES(true) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS matches (
  id UUID PRIMARY KEY,
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  provider_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider_type TEXT NOT NULL CHECK(provider_type IN ('isp','technician')),
  rating NUMERIC(5,2) NOT NULL DEFAULT 0,
  distance_km NUMERIC(12,3) NOT NULL DEFAULT 0,
  price_estimate NUMERIC(14,2) NOT NULL DEFAULT 0,
  match_score NUMERIC(8,5) NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'notified' CHECK(status IN ('notified','seen','declined','expired','assigned')),
  notified_at TIMESTAMPTZ,
  seen_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(request_id, provider_id)
);
CREATE INDEX IF NOT EXISTS matches_request_status_idx ON matches(request_id,status,match_score DESC);
CREATE INDEX IF NOT EXISTS matches_expiry_idx ON matches(notified_at,status);

COMMIT;
