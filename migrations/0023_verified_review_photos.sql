BEGIN;

ALTER TABLE reviews ADD COLUMN IF NOT EXISTS request_id UUID REFERENCES service_requests(id) ON DELETE SET NULL;
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS photo_urls JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS is_verified BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS reviews_verified_idx ON reviews(target_id,target_type,created_at DESC) WHERE is_verified=true;

COMMIT;
