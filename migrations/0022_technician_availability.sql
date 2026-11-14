BEGIN;

CREATE TABLE IF NOT EXISTS technician_availability_rules (
  id UUID PRIMARY KEY,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  weekday SMALLINT NOT NULL CHECK(weekday BETWEEN 0 AND 6),
  start_time TIME NOT NULL,
  end_time TIME NOT NULL,
  timezone TEXT NOT NULL DEFAULT 'Africa/Nairobi',
  active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(start_time < end_time),
  UNIQUE(technician_id,weekday,start_time,end_time)
);
CREATE INDEX IF NOT EXISTS technician_availability_rules_lookup_idx
  ON technician_availability_rules(technician_id,weekday) WHERE active=true;

CREATE TABLE IF NOT EXISTS technician_availability_exceptions (
  id UUID PRIMARY KEY,
  technician_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  exception_date DATE NOT NULL,
  available BOOLEAN NOT NULL DEFAULT false,
  start_time TIME,
  end_time TIME,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK((start_time IS NULL AND end_time IS NULL) OR (start_time IS NOT NULL AND end_time IS NOT NULL AND start_time < end_time)),
  UNIQUE(technician_id,exception_date)
);

COMMIT;
