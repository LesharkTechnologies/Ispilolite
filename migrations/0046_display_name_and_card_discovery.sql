BEGIN;

-- Add display_name to users, technicians, technician_profiles, and isps
-- Users can set a public display name / moniker (e.g. "Leshark") while their legal/verified
-- identity is preserved (e.g. identity [Collins Muthomi]).

ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE technicians ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE technician_profiles ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE isps ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS users_display_name_idx ON users(display_name) WHERE display_name <> '';
CREATE INDEX IF NOT EXISTS technicians_display_name_idx ON technicians(display_name) WHERE display_name <> '';

COMMIT;
