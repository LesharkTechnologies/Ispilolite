BEGIN;

CREATE TABLE IF NOT EXISTS service_request_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT '',
  actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
  data JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS service_request_events_request_idx
  ON service_request_events(request_id, created_at DESC);

CREATE TABLE IF NOT EXISTS technician_live_locations (
  technician_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  latitude DOUBLE PRECISION NOT NULL CHECK(latitude BETWEEN -90 AND 90),
  longitude DOUBLE PRECISION NOT NULL CHECK(longitude BETWEEN -180 AND 180),
  accuracy_m DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK(accuracy_m >= 0),
  heading DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK(heading >= 0 AND heading <= 360),
  speed_kph DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK(speed_kph >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS technician_live_locations_request_idx
  ON technician_live_locations(request_id, updated_at DESC);

-- Record lifecycle changes even when they originate outside the Go API.
CREATE OR REPLACE FUNCTION record_service_request_status_event() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
    INSERT INTO service_request_events(request_id,event_type,status,data,created_at)
    VALUES (NEW.id,'job_status',NEW.status,
      jsonb_build_object('previous_status',CASE WHEN TG_OP='UPDATE' THEN OLD.status ELSE NULL END),now());
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS service_request_status_event_trigger ON service_requests;
CREATE TRIGGER service_request_status_event_trigger
AFTER INSERT OR UPDATE OF status ON service_requests
FOR EACH ROW EXECUTE FUNCTION record_service_request_status_event();

COMMIT;
