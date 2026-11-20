BEGIN;

DO $$
DECLARE legacy_prefix TEXT := 'cloud' || 'inary_';
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='documents' AND column_name=legacy_prefix || 'public_id') THEN
    EXECUTE format('ALTER TABLE documents RENAME COLUMN %I TO object_key', legacy_prefix || 'public_id');
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='documents' AND column_name=legacy_prefix || 'url') THEN
    EXECUTE format('ALTER TABLE documents RENAME COLUMN %I TO media_url', legacy_prefix || 'url');
  END IF;
END $$;

ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS photo_urls TEXT[] NOT NULL DEFAULT '{}';

COMMIT;
