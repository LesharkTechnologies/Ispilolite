BEGIN;

CREATE TABLE IF NOT EXISTS isp_technicians (
  id UUID PRIMARY KEY,
  isp_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  company_technician_id TEXT NOT NULL,
  phone_snapshot TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','ended','removed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(isp_id,technician_id),
  UNIQUE(isp_id,company_technician_id)
);
CREATE INDEX IF NOT EXISTS isp_technicians_technician_idx ON isp_technicians(technician_id,status);

CREATE TABLE IF NOT EXISTS isp_vacancies (
  id UUID PRIMARY KEY,
  isp_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  service_type TEXT NOT NULL,
  slots INTEGER NOT NULL DEFAULT 1 CHECK(slots > 0),
  deadline_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','closed','expired','deleted')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS isp_vacancies_open_idx ON isp_vacancies(service_type,deadline_at) WHERE status='open';

CREATE TABLE IF NOT EXISTS isp_vacancy_applications (
  id UUID PRIMARY KEY,
  vacancy_id UUID NOT NULL REFERENCES isp_vacancies(id) ON DELETE CASCADE,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  message TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','rejected','withdrawn')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(vacancy_id,technician_id)
);

CREATE TABLE IF NOT EXISTS work_engagements (
  id UUID PRIMARY KEY,
  customer_id UUID REFERENCES users(id) ON DELETE SET NULL,
  isp_id UUID REFERENCES users(id) ON DELETE SET NULL,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  request_id UUID REFERENCES service_requests(id) ON DELETE SET NULL,
  vacancy_id UUID REFERENCES isp_vacancies(id) ON DELETE SET NULL,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expected_end_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  termination_reason TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','completed','terminated','cancelled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(customer_id IS NOT NULL OR isp_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS work_engagements_technician_idx ON work_engagements(technician_id,status,started_at DESC);
CREATE INDEX IF NOT EXISTS work_engagements_isp_idx ON work_engagements(isp_id,status,started_at DESC);

ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS terminated_at TIMESTAMPTZ;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS termination_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE reviews ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS engagement_id UUID REFERENCES work_engagements(id) ON DELETE SET NULL;

COMMIT;
