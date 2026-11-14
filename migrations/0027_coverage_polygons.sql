BEGIN;

CREATE TABLE IF NOT EXISTS isp_coverage_polygons (
  id UUID PRIMARY KEY,
  isp_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  county TEXT NOT NULL DEFAULT '',
  geojson JSONB NOT NULL,
  min_lat DOUBLE PRECISION NOT NULL,
  max_lat DOUBLE PRECISION NOT NULL,
  min_lon DOUBLE PRECISION NOT NULL,
  max_lon DOUBLE PRECISION NOT NULL,
  stroke_color TEXT NOT NULL DEFAULT '#2563EB',
  fill_color TEXT NOT NULL DEFAULT '#3B82F6',
  active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(min_lat<=max_lat AND min_lon<=max_lon)
);
CREATE INDEX IF NOT EXISTS isp_coverage_polygons_bbox_idx
  ON isp_coverage_polygons(min_lat,max_lat,min_lon,max_lon) WHERE active=true;
CREATE INDEX IF NOT EXISTS isp_coverage_polygons_county_idx
  ON isp_coverage_polygons(lower(county)) WHERE active=true;

COMMIT;
