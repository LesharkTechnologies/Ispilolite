BEGIN;

-- Operational fields for the request-to-install pipeline.
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS response_due_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS quote_due_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS installation_due_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS viewed_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS loss_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE service_requests DROP CONSTRAINT IF EXISTS service_requests_status_check;
ALTER TABLE service_requests ADD CONSTRAINT service_requests_status_check
  CHECK(status IN ('open','viewed','quoted','assigned','accepted','scheduled','in_progress','completed','activated','unavailable','cancelled','deleted','lost'));

CREATE OR REPLACE FUNCTION initialize_service_request_sla() RETURNS trigger AS $$
BEGIN
  NEW.response_due_at := COALESCE(NEW.response_due_at, now() + interval '2 hours');
  NEW.quote_due_at := COALESCE(NEW.quote_due_at, now() + interval '24 hours');
  NEW.installation_due_at := COALESCE(NEW.installation_due_at, NEW.preferred_date, now() + interval '3 days');
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS service_request_sla_defaults_trigger ON service_requests;
CREATE TRIGGER service_request_sla_defaults_trigger
BEFORE INSERT ON service_requests
FOR EACH ROW EXECUTE FUNCTION initialize_service_request_sla();

CREATE OR REPLACE FUNCTION update_service_request_workflow_timestamps() RETURNS trigger AS $$
BEGIN
  IF NEW.status = 'viewed' AND OLD.status <> 'viewed' THEN NEW.viewed_at := COALESCE(NEW.viewed_at, now()); END IF;
  IF NEW.status = 'accepted' AND OLD.status <> 'accepted' THEN NEW.accepted_at := COALESCE(NEW.accepted_at, now()); END IF;
  IF NEW.status = 'assigned' AND OLD.status <> 'assigned' THEN NEW.scheduled_at := COALESCE(NEW.scheduled_at, now()); END IF;
  IF NEW.status = 'completed' AND OLD.status <> 'completed' THEN NEW.completed_at := COALESCE(NEW.completed_at, now()); END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS service_request_workflow_timestamps_trigger ON service_requests;
CREATE TRIGGER service_request_workflow_timestamps_trigger
BEFORE UPDATE OF status ON service_requests
FOR EACH ROW EXECUTE FUNCTION update_service_request_workflow_timestamps();

UPDATE service_requests
SET response_due_at = COALESCE(response_due_at, created_at + interval '2 hours'),
    quote_due_at = COALESCE(quote_due_at, created_at + interval '24 hours'),
    installation_due_at = COALESCE(installation_due_at, preferred_date, created_at + interval '3 days')
WHERE response_due_at IS NULL OR quote_due_at IS NULL OR installation_due_at IS NULL;

CREATE TABLE IF NOT EXISTS isp_serviceability (
  id UUID PRIMARY KEY,
  isp_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  location_id UUID REFERENCES locations(id) ON DELETE CASCADE,
  county TEXT NOT NULL DEFAULT '',
  town TEXT NOT NULL DEFAULT '',
  estate TEXT NOT NULL DEFAULT '',
  building TEXT NOT NULL DEFAULT '',
  technology TEXT NOT NULL DEFAULT 'fibre',
  status TEXT NOT NULL DEFAULT 'serviceable' CHECK(status IN ('serviceable','likely_serviceable','survey_required','not_serviceable')),
  estimated_days INTEGER NOT NULL DEFAULT 3 CHECK(estimated_days BETWEEN 0 AND 365),
  reason TEXT NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS isp_serviceability_scope_uidx
  ON isp_serviceability(isp_id,COALESCE(location_id::text,''),lower(county),lower(town),lower(estate),lower(building));
CREATE INDEX IF NOT EXISTS isp_serviceability_lookup_idx
  ON isp_serviceability(lower(county),lower(town),lower(estate),active,status);

CREATE TABLE IF NOT EXISTS serviceability_waitlist (
  id UUID PRIMARY KEY,
  customer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  isp_id UUID REFERENCES users(id) ON DELETE CASCADE,
  location_id UUID REFERENCES locations(id) ON DELETE SET NULL,
  county TEXT NOT NULL DEFAULT '',
  town TEXT NOT NULL DEFAULT '',
  estate TEXT NOT NULL DEFAULT '',
  building TEXT NOT NULL DEFAULT '',
  service_type TEXT NOT NULL DEFAULT 'internet',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  notified_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS serviceability_waitlist_isp_idx
  ON serviceability_waitlist(isp_id,created_at DESC);
CREATE INDEX IF NOT EXISTS serviceability_waitlist_area_idx
  ON serviceability_waitlist(lower(county),lower(town),lower(estate),created_at DESC);

CREATE TABLE IF NOT EXISTS installation_proofs (
  id UUID PRIMARY KEY,
  request_id UUID NOT NULL UNIQUE REFERENCES service_requests(id) ON DELETE CASCADE,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  check_in_at TIMESTAMPTZ NOT NULL,
  check_out_at TIMESTAMPTZ,
  latitude DOUBLE PRECISION,
  longitude DOUBLE PRECISION,
  photo_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
  checklist JSONB NOT NULL DEFAULT '{}'::jsonb,
  download_mbps NUMERIC(10,2),
  upload_mbps NUMERIC(10,2),
  latency_ms NUMERIC(10,2),
  packet_loss_percent NUMERIC(6,2),
  device_serial TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'submitted' CHECK(status IN ('submitted','approved','rework')),
  customer_confirmed_at TIMESTAMPTZ,
  customer_confirmed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS installation_proofs_technician_idx
  ON installation_proofs(technician_id,created_at DESC);

-- Customer acceptance of an ISP quotation moves the linked request into the
-- accepted stage and gives the operations dashboard a reliable timestamp.
CREATE OR REPLACE FUNCTION update_request_after_quotation() RETURNS trigger AS $$
BEGIN
  IF NEW.request_id IS NOT NULL AND NEW.status IN ('FINALIZED','SENT','VIEWED','ACCEPTED') THEN
    UPDATE service_requests
    SET status = CASE
          WHEN NEW.status = 'ACCEPTED' AND status IN ('open','viewed','quoted','assigned','accepted') THEN 'accepted'
          WHEN NEW.status <> 'ACCEPTED' AND status IN ('open','viewed') THEN 'quoted'
          ELSE status END,
        is_available = false,
        accepted_at = CASE WHEN NEW.status = 'ACCEPTED' THEN COALESCE(accepted_at, now()) ELSE accepted_at END,
        updated_at = now()
    WHERE id = NEW.request_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS quotation_request_workflow_trigger ON quotations;
CREATE TRIGGER quotation_request_workflow_trigger
AFTER INSERT OR UPDATE OF status ON quotations
FOR EACH ROW EXECUTE FUNCTION update_request_after_quotation();

COMMIT;
