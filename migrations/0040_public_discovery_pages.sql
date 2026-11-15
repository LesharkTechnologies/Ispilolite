BEGIN;

-- Anonymous, aggregate-only demand signal for public coverage pages. No IP
-- addresses, user agents or identifiers are stored.
CREATE TABLE IF NOT EXISTS coverage_page_views (
  id UUID PRIMARY KEY,
  page_type TEXT NOT NULL CHECK(page_type IN ('county','town','estate')),
  page_key TEXT NOT NULL,
  referrer TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS coverage_page_views_page_idx
  ON coverage_page_views(page_type,page_key,created_at DESC);
CREATE INDEX IF NOT EXISTS coverage_page_views_recent_idx
  ON coverage_page_views(created_at DESC);

COMMIT;
