BEGIN;

-- Trust & safety: customer-submitted reports about other users.
-- A report stays hidden from the reported user. Only admins review it.
CREATE TABLE IF NOT EXISTS customer_flags (
  id UUID PRIMARY KEY,
  reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  -- Either a registered user or a phone number typed by the reporter. The
  -- phone path never creates or impersonates an account.
  reported_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  reported_phone TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL CHECK(category IN (
    'fraud','harassment','impersonation','spam','payment_scam',
    'fake_listing','abusive_content','other'
  )),
  description TEXT NOT NULL CHECK(char_length(description) BETWEEN 10 AND 4000),
  image_urls JSONB NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_array_length(image_urls) <= 3),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending','in_progress','approved','cancelled')),
  admin_feedback TEXT NOT NULL DEFAULT '',
  reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  reviewed_at TIMESTAMPTZ,
  -- Action taken when a report is approved.
  account_action TEXT NOT NULL DEFAULT 'none'
    CHECK(account_action IN ('none','monitor','suspend')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(reported_user_id IS NOT NULL OR reported_phone <> '')
);
CREATE UNIQUE INDEX IF NOT EXISTS customer_flags_open_uidx
  ON customer_flags(reporter_id, reported_user_id)
  WHERE status IN ('pending','in_progress') AND reported_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS customer_flags_admin_idx
  ON customer_flags(status, created_at);
CREATE INDEX IF NOT EXISTS customer_flags_user_idx
  ON customer_flags(reported_user_id, created_at DESC);

-- Users added to the verification/monitoring list. One row per user; release
-- deactivates the row instead of deleting it so the history is preserved.
CREATE TABLE IF NOT EXISTS user_monitoring (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  flag_id UUID REFERENCES customer_flags(id) ON DELETE SET NULL,
  reason TEXT NOT NULL DEFAULT '',
  added_by UUID REFERENCES users(id) ON DELETE SET NULL,
  active BOOLEAN NOT NULL DEFAULT true,
  released_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS user_monitoring_active_idx ON user_monitoring(active, created_at DESC);

-- Activity captured only for monitored users: job applications, job claims and
-- client-detail views. The insert is a no-op for anyone not monitored.
CREATE TABLE IF NOT EXISTS monitored_activity (
  id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  event TEXT NOT NULL CHECK(event IN ('job_application','job_claim','client_view','contact_attempt')),
  subject_type TEXT NOT NULL DEFAULT '',
  subject_id TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  ip_address INET,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS monitored_activity_user_idx
  ON monitored_activity(user_id, created_at DESC);

COMMIT;
