BEGIN;

CREATE TABLE IF NOT EXISTS speed_tests (
  id UUID PRIMARY KEY,
  test_session_id UUID NOT NULL,
  user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  isp_id UUID REFERENCES isps(id) ON DELETE SET NULL,
  isp_name TEXT NOT NULL DEFAULT '',
  isp_selection_source TEXT NOT NULL DEFAULT 'user_selected'
    CHECK (isp_selection_source IN ('registered','user_entered','none')),
  county TEXT NOT NULL DEFAULT '',
  town TEXT NOT NULL DEFAULT '',
  download_mbps NUMERIC(10,2) NOT NULL CHECK (download_mbps >= 0 AND download_mbps <= 100000),
  upload_mbps NUMERIC(10,2) NOT NULL CHECK (upload_mbps >= 0 AND upload_mbps <= 100000),
  latency_ms NUMERIC(10,2) NOT NULL CHECK (latency_ms >= 0 AND latency_ms <= 60000),
  jitter_ms NUMERIC(10,2) CHECK (jitter_ms IS NULL OR (jitter_ms >= 0 AND jitter_ms <= 60000)),
  packet_loss_percent NUMERIC(6,2) CHECK (packet_loss_percent IS NULL OR (packet_loss_percent >= 0 AND packet_loss_percent <= 100)),
  isp_plan_speed_mbps NUMERIC(10,2) CHECK (isp_plan_speed_mbps IS NULL OR (isp_plan_speed_mbps >= 0 AND isp_plan_speed_mbps <= 100000)),
  include_in_public_stats BOOLEAN NOT NULL DEFAULT false,
  ip_address INET NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','accepted','rejected','suspicious')),
  rejection_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS speed_tests_user_created_idx
  ON speed_tests(user_id,created_at DESC);
CREATE INDEX IF NOT EXISTS speed_tests_isp_area_idx
  ON speed_tests(isp_id,lower(county),lower(town),created_at DESC);
CREATE INDEX IF NOT EXISTS speed_tests_public_idx
  ON speed_tests(isp_id,include_in_public_stats,status,created_at DESC);
CREATE INDEX IF NOT EXISTS speed_tests_session_idx
  ON speed_tests(test_session_id);

COMMIT;
