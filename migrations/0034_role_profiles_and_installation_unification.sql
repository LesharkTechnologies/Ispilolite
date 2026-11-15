BEGIN;

-- Every role account must have the row used by its public/catalog APIs.
INSERT INTO isps(id,name,avatar_url,logo_url,customer_care_number,rating,review_count,is_active,county,created_at,updated_at)
SELECT u.id,u.name,COALESCE(u.avatar_url,''),COALESCE(u.avatar_url,''),u.phone,u.rating,u.total_ratings,u.status='active',u.county,u.created_at,u.updated_at
FROM users u
WHERE u.role='isp'
ON CONFLICT(id) DO UPDATE SET
  name=EXCLUDED.name,
  avatar_url=CASE WHEN isps.avatar_url='' THEN EXCLUDED.avatar_url ELSE isps.avatar_url END,
  logo_url=CASE WHEN isps.logo_url='' THEN EXCLUDED.logo_url ELSE isps.logo_url END,
  rating=EXCLUDED.rating,
  review_count=EXCLUDED.review_count,
  is_active=EXCLUDED.is_active,
  county=CASE WHEN isps.county='' THEN EXCLUDED.county ELSE isps.county END,
  updated_at=GREATEST(isps.updated_at,EXCLUDED.updated_at);

INSERT INTO technicians(id,user_id,name,avatar_url,rating,jobs_done,is_available,county,lat,lon,created_at,updated_at)
SELECT u.id,u.id,u.name,COALESCE(u.avatar_url,''),u.rating,0,true,u.county,u.latitude,u.longitude,u.created_at,u.updated_at
FROM users u
WHERE u.role='technician'
  AND NOT EXISTS (SELECT 1 FROM technicians t WHERE t.user_id=u.id OR t.id=u.id)
ON CONFLICT(id) DO NOTHING;

UPDATE technicians t SET
  user_id=COALESCE(t.user_id,u.id),
  name=u.name,
  avatar_url=CASE WHEN COALESCE(t.avatar_url,'')='' THEN COALESCE(u.avatar_url,'') ELSE t.avatar_url END,
  rating=u.rating,
  county=CASE WHEN t.county='' THEN u.county ELSE t.county END,
  lat=COALESCE(t.lat,u.latitude),lon=COALESCE(t.lon,u.longitude),updated_at=GREATEST(t.updated_at,u.updated_at)
FROM users u
WHERE u.role='technician' AND (t.user_id=u.id OR t.id=u.id);

CREATE INDEX IF NOT EXISTS technicians_user_id_idx
  ON technicians(user_id) WHERE user_id IS NOT NULL;

INSERT INTO technician_profiles(technician_id,county,town,is_available,updated_at)
SELECT u.id,u.county,u.town,true,u.updated_at
FROM users u
WHERE u.role='technician'
ON CONFLICT(technician_id) DO NOTHING;

-- Preserve legacy installation records while making service_requests the one
-- source of truth used by every installation/job endpoint.
INSERT INTO service_requests(
  id,customer_id,requester_role,request_type,mode,target_isp_id,
  assigned_isp_id,assigned_technician_id,location_id,service_type,
  description,budget,preferred_date,status,is_available,created_at,updated_at
)
SELECT i.id,i.client_id,'customer','internet',
  CASE WHEN i.isp_id IS NULL THEN 'broadcast' ELSE 'direct' END,
  i.isp_id,
  CASE WHEN i.status IN ('accepted','assigned','in_progress','completed') THEN i.isp_id ELSE NULL END,
  i.technician_id,i.location_id,i.service_type,i.description,i.budget,
  i.preferred_date,
  CASE WHEN i.status='pending' THEN 'open' WHEN i.status='rejected' THEN 'cancelled' ELSE i.status END,
  i.status IN ('pending'),i.created_at,i.updated_at
FROM installations i
WHERE i.client_id IS NOT NULL
ON CONFLICT(id) DO NOTHING;

INSERT INTO work_engagements(id,customer_id,isp_id,technician_id,request_id,started_at,ended_at,status)
SELECT gen_random_uuid(),s.customer_id,COALESCE(s.assigned_isp_id,s.target_isp_id),
  COALESCE(s.assigned_technician_id,s.target_technician_id),s.id,s.updated_at,
  CASE WHEN s.status='completed' THEN s.updated_at ELSE NULL END,
  CASE WHEN s.status='completed' THEN 'completed' ELSE 'active' END
FROM service_requests s
WHERE s.deleted_at IS NULL
  AND s.status IN ('assigned','accepted','in_progress','completed')
  AND COALESCE(s.assigned_technician_id,s.target_technician_id) IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM work_engagements w
    WHERE w.request_id=s.id
      AND w.technician_id=COALESCE(s.assigned_technician_id,s.target_technician_id)
  );

COMMIT;
