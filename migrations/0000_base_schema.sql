-- Baseline tables required by the versioned migrations on a fresh database.
-- Existing installations retain their tables (CREATE IF NOT EXISTS).
-- No PostGIS dependency: current runtime geospatial queries use numeric
-- latitude/longitude; search uses pg_trgm and earthdistance.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS cube;
CREATE EXTENSION IF NOT EXISTS earthdistance;

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY,
  phone TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL CHECK (role IN ('customer','isp','technician','admin')),
  password_hash TEXT NOT NULL DEFAULT '',
  is_verified BOOLEAN NOT NULL DEFAULT false,
  rating DOUBLE PRECISION NOT NULL DEFAULT 0,
  total_ratings INTEGER NOT NULL DEFAULT 0,
  joined TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  isp_id UUID REFERENCES users(id) ON DELETE SET NULL,
  latitude DOUBLE PRECISION,
  longitude DOUBLE PRECISION,
  town TEXT NOT NULL DEFAULT '',
  county TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS locations (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS isps (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  logo_url TEXT NOT NULL DEFAULT '',
  customer_care_number TEXT NOT NULL DEFAULT '',
  technicians_available INTEGER NOT NULL DEFAULT 0,
  avg_response_time INTEGER NOT NULL DEFAULT 0,
  avg_price NUMERIC(14,2) NOT NULL DEFAULT 0,
  rating DOUBLE PRECISION NOT NULL DEFAULT 0,
  review_count INTEGER NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT true,
  county TEXT NOT NULL DEFAULT '',
  sub_county TEXT NOT NULL DEFAULT '',
  village TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS technicians (
  id UUID PRIMARY KEY,
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL DEFAULT '',
  isp_id UUID REFERENCES isps(id) ON DELETE SET NULL,
  location_id UUID REFERENCES locations(id) ON DELETE SET NULL,
  isp_name TEXT NOT NULL DEFAULT '',
  rating DOUBLE PRECISION NOT NULL DEFAULT 0,
  jobs_done INTEGER NOT NULL DEFAULT 0,
  is_available BOOLEAN NOT NULL DEFAULT true,
  county TEXT NOT NULL DEFAULT '',
  sub_county TEXT NOT NULL DEFAULT '',
  village TEXT NOT NULL DEFAULT '',
  lat DOUBLE PRECISION,
  lon DOUBLE PRECISION,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS technician_roles (
  technician_id UUID NOT NULL REFERENCES technicians(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  PRIMARY KEY (technician_id, role)
);
CREATE TABLE IF NOT EXISTS technician_skills (
  technician_id UUID NOT NULL REFERENCES technicians(id) ON DELETE CASCADE,
  skill TEXT NOT NULL,
  PRIMARY KEY (technician_id, skill)
);

CREATE TABLE IF NOT EXISTS installations (
  id UUID PRIMARY KEY,
  location_id UUID REFERENCES locations(id) ON DELETE SET NULL,
  service_type TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  preferred_date TIMESTAMPTZ,
  budget NUMERIC(14,2) NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  client_id UUID REFERENCES users(id) ON DELETE SET NULL,
  isp_id UUID REFERENCES users(id) ON DELETE SET NULL,
  technician_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS isp_packages (
  id UUID PRIMARY KEY,
  isp_id UUID NOT NULL REFERENCES isps(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  speed TEXT,
  price NUMERIC(14,2),
  description TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
