-- Store a stable, domain-free relative link for every account. The browser
-- origin is supplied by PROFILE_PUBLIC_BASE_URL when responding to requests.
-- Slugs retain the account's creation year and full UUID, so display-name
-- changes cannot break links and identical names never collide.
ALTER TABLE users ADD COLUMN IF NOT EXISTS profile_slug TEXT;

CREATE OR REPLACE FUNCTION ispilo_make_profile_slug(account_name TEXT, created TIMESTAMPTZ, account_id UUID)
RETURNS TEXT LANGUAGE SQL STABLE AS $$
  SELECT coalesce(nullif(btrim(regexp_replace(lower(left(coalesce(account_name,''),40)), '[^a-z0-9]+', '-', 'g'),'-'),''),'user')
    || '-' || to_char(created AT TIME ZONE 'UTC','YYYY')
    || '-' || replace(account_id::text,'-','');
$$;

UPDATE users SET profile_slug=ispilo_make_profile_slug(name, created_at, id)
WHERE profile_slug IS NULL OR profile_slug='';

ALTER TABLE users ALTER COLUMN profile_slug SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_profile_slug_uidx ON users(profile_slug);

-- STORED generated path is persisted with the user, but contains no hostname.
ALTER TABLE users ADD COLUMN IF NOT EXISTS profile_path TEXT
  GENERATED ALWAYS AS ('/u/' || profile_slug) STORED;

CREATE OR REPLACE FUNCTION ispilo_assign_profile_slug() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    NEW.profile_slug := ispilo_make_profile_slug(NEW.name, NEW.created_at, NEW.id);
  ELSE
    -- URLs remain stable after renames, even if application code tries to
    -- replace the slug. Account deletion keeps the path reserved.
    NEW.profile_slug := OLD.profile_slug;
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS users_profile_slug_insert ON users;
CREATE TRIGGER users_profile_slug_insert BEFORE INSERT ON users
FOR EACH ROW EXECUTE FUNCTION ispilo_assign_profile_slug();

DROP TRIGGER IF EXISTS users_profile_slug_immutable ON users;
CREATE TRIGGER users_profile_slug_immutable BEFORE UPDATE OF profile_slug ON users
FOR EACH ROW EXECUTE FUNCTION ispilo_assign_profile_slug();
