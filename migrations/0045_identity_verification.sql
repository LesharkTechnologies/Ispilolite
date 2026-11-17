BEGIN;

-- Identity verification system for technicians, ISPs, and users.
-- Technicians upload an ID photo linked to the phone number they verified their account with.
-- Admins verify identity, set verified technician name, and assign status:
-- 'id_verified', 'business_verified', or 'both' (when technician & ISP).

CREATE TABLE IF NOT EXISTS identity_verifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  phone TEXT NOT NULL,
  id_photo_url TEXT NOT NULL,
  id_photo_back_url TEXT NOT NULL DEFAULT '',
  id_number TEXT NOT NULL DEFAULT '',
  document_type TEXT NOT NULL DEFAULT 'national_id'
    CHECK (document_type IN ('national_id', 'passport', 'business_permit', 'driving_license', 'other')),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'approved', 'rejected', 'in_review')),
  verification_badge TEXT NOT NULL DEFAULT 'unverified'
    CHECK (verification_badge IN ('unverified', 'id_verified', 'business_verified', 'both', 'rejected')),
  verified_name TEXT NOT NULL DEFAULT '',
  rejection_reason TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS identity_verifications_user_idx
  ON identity_verifications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS identity_verifications_status_idx
  ON identity_verifications(status, created_at DESC);
CREATE INDEX IF NOT EXISTS identity_verifications_badge_idx
  ON identity_verifications(verification_badge, created_at DESC);
CREATE INDEX IF NOT EXISTS identity_verifications_phone_idx
  ON identity_verifications(phone);

-- Add identity_status and verified_name to users
ALTER TABLE users ADD COLUMN IF NOT EXISTS identity_status TEXT NOT NULL DEFAULT 'unverified'
  CHECK (identity_status IN ('unverified', 'pending', 'id_verified', 'business_verified', 'both', 'rejected'));
ALTER TABLE users ADD COLUMN IF NOT EXISTS verified_name TEXT NOT NULL DEFAULT '';

-- Add identity_status and verified_name to technicians
ALTER TABLE technicians ADD COLUMN IF NOT EXISTS identity_status TEXT NOT NULL DEFAULT 'unverified'
  CHECK (identity_status IN ('unverified', 'pending', 'id_verified', 'business_verified', 'both', 'rejected'));
ALTER TABLE technicians ADD COLUMN IF NOT EXISTS verified_name TEXT NOT NULL DEFAULT '';

-- Add identity_status and verified_name to technician_profiles
ALTER TABLE technician_profiles ADD COLUMN IF NOT EXISTS identity_status TEXT NOT NULL DEFAULT 'unverified'
  CHECK (identity_status IN ('unverified', 'pending', 'id_verified', 'business_verified', 'both', 'rejected'));
ALTER TABLE technician_profiles ADD COLUMN IF NOT EXISTS verified_name TEXT NOT NULL DEFAULT '';

-- Add identity_status and verified_name to isps
ALTER TABLE isps ADD COLUMN IF NOT EXISTS identity_status TEXT NOT NULL DEFAULT 'unverified'
  CHECK (identity_status IN ('unverified', 'pending', 'id_verified', 'business_verified', 'both', 'rejected'));
ALTER TABLE isps ADD COLUMN IF NOT EXISTS verified_name TEXT NOT NULL DEFAULT '';

COMMIT;
