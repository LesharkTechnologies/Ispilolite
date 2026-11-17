BEGIN;

-- Stores aggregated internet speed metrics per county and town on a monthly basis.
-- This powers speed recommendations, regional comparisons (e.g. Machakos Town 5-50 Mbps, avg 30 Mbps),
-- and county benchmark leaderboards (e.g. Nairobi, Mombasa, Kisumu).
CREATE TABLE IF NOT EXISTS county_monthly_speed_stats (
  id UUID PRIMARY KEY,
  year INT NOT NULL CHECK (year >= 2020 AND year <= 2100),
  month INT NOT NULL CHECK (month >= 1 AND month <= 12),
  county TEXT NOT NULL,
  town TEXT NOT NULL DEFAULT '',
  isp_id UUID REFERENCES users(id) ON DELETE SET NULL,
  isp_name TEXT NOT NULL DEFAULT '',
  sample_count INT NOT NULL DEFAULT 0,
  min_download_mbps NUMERIC(10,2) NOT NULL DEFAULT 0,
  max_download_mbps NUMERIC(10,2) NOT NULL DEFAULT 0,
  avg_download_mbps NUMERIC(10,2) NOT NULL DEFAULT 0,
  min_upload_mbps NUMERIC(10,2) NOT NULL DEFAULT 0,
  max_upload_mbps NUMERIC(10,2) NOT NULL DEFAULT 0,
  avg_upload_mbps NUMERIC(10,2) NOT NULL DEFAULT 0,
  avg_latency_ms NUMERIC(10,2) NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Unique index to prevent duplicate monthly aggregate records for the same scope
CREATE UNIQUE INDEX IF NOT EXISTS county_monthly_speed_stats_scope_uidx
  ON county_monthly_speed_stats(year, month, lower(county), lower(town), COALESCE(isp_id, '00000000-0000-0000-0000-000000000000'::uuid));

CREATE INDEX IF NOT EXISTS county_monthly_speed_stats_lookup_idx
  ON county_monthly_speed_stats(lower(county), lower(town), year DESC, month DESC);

CREATE INDEX IF NOT EXISTS county_monthly_speed_stats_benchmarks_idx
  ON county_monthly_speed_stats(year DESC, month DESC, avg_download_mbps DESC);

COMMIT;
