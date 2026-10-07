BEGIN;
-- Permit revoking a historical non-owner membership after its Person was
-- suspended/deleted. No inactive Person can acquire or change active authority.
CREATE OR REPLACE FUNCTION birdtie_validate_business_member() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.id=OLD.id AND NEW.business_id=OLD.business_id AND
           NEW.user_account_id=OLD.user_account_id AND NEW.role=OLD.role AND
           NEW.created_at=OLD.created_at AND NEW.status='removed' AND OLD.role<>'owner' AND
           EXISTS(SELECT 1 FROM accounts WHERE id=NEW.user_account_id AND account_type='person') THEN
            RETURN NEW;
        END IF;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.user_account_id
                  AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Business membership requires active Person';
    END IF;
    RETURN NEW;
END $$;
-- Business identity/operation verification lasts until explicit revocation.
-- Finite reviewer grants authorize the review; finite fact freshness does not
-- silently redefine the original041 identity or public Venue authority.
CREATE FUNCTION birdtie_business_https_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT length(value) BETWEEN 9 AND 2048
 AND value ~ '^https://[^/@#[:space:]]+(/[^#[:space:]]*)?$';
$$;

CREATE FUNCTION birdtie_business_profile_valid(f jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE STRICT AS $$
DECLARE d jsonb; seen_days integer[]:='{}'; day integer; opening text; closing text;
BEGIN
 IF jsonb_typeof(f) IS DISTINCT FROM 'object' OR
    (SELECT count(*) FROM jsonb_object_keys(f))<>5 OR
    NOT(f ?& ARRAY['name','description','timeZone','openingHours','officialLinks']) OR
    jsonb_typeof(f->'name') IS DISTINCT FROM 'string' OR
    length(f->>'name') NOT BETWEEN 1 AND 160 OR btrim(f->>'name') IS DISTINCT FROM f->>'name' OR
    jsonb_typeof(f->'description') IS DISTINCT FROM 'string' OR length(f->>'description')>3000 OR
    jsonb_typeof(f->'timeZone') IS DISTINCT FROM 'string' OR length(f->>'timeZone')>100 OR
    jsonb_typeof(f->'openingHours') IS DISTINCT FROM 'array' OR
    jsonb_typeof(f->'officialLinks') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 IF jsonb_array_length(f->'openingHours')>7 OR jsonb_array_length(f->'officialLinks')>8 THEN RETURN false; END IF;
 IF (f->>'timeZone')<>'' AND NOT EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=f->>'timeZone') THEN RETURN false; END IF;
 IF jsonb_array_length(f->'openingHours')>0 AND f->>'timeZone'='' THEN RETURN false; END IF;
 FOR d IN SELECT value FROM jsonb_array_elements(f->'openingHours') LOOP
  IF jsonb_typeof(d) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(d))<>5 OR
     NOT(d ?& ARRAY['day','closed','opensAt','closesAt','nextDay']) OR
     jsonb_typeof(d->'day') IS DISTINCT FROM 'number' OR (d->>'day') !~ '^[1-7]$' OR
     jsonb_typeof(d->'closed') IS DISTINCT FROM 'boolean' OR jsonb_typeof(d->'nextDay') IS DISTINCT FROM 'boolean' OR
     jsonb_typeof(d->'opensAt') IS DISTINCT FROM 'string' OR jsonb_typeof(d->'closesAt') IS DISTINCT FROM 'string'
  THEN RETURN false; END IF;
  day:=(d->>'day')::integer;
  IF day=ANY(seen_days) THEN RETURN false; END IF; seen_days:=array_append(seen_days,day);
  opening:=d->>'opensAt';closing:=d->>'closesAt';
  IF (d->>'closed')::boolean THEN
   IF opening<>'' OR closing<>'' OR (d->>'nextDay')::boolean THEN RETURN false; END IF;
  ELSE
   IF opening !~ '^(0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]$' OR closing !~ '^(0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]$' OR
      (NOT (d->>'nextDay')::boolean AND closing<=opening) OR ((d->>'nextDay')::boolean AND closing>opening) THEN RETURN false; END IF;
  END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(f->'officialLinks') x WHERE jsonb_typeof(x) IS DISTINCT FROM 'string' OR NOT birdtie_business_https_valid(x#>>'{}')) OR
    (SELECT count(*) FROM jsonb_array_elements(f->'officialLinks'))<>(SELECT count(DISTINCT x) FROM jsonb_array_elements(f->'officialLinks') x)
 THEN RETURN false; END IF;
 RETURN true;
END $$;

CREATE FUNCTION birdtie_business_venue_facts_valid(f jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
BEGIN
 IF jsonb_typeof(f) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(f))<>3 OR
    NOT(f ?& ARRAY['suitability','bookingUrl','note']) OR
    jsonb_typeof(f->'suitability') IS DISTINCT FROM 'array' OR
    jsonb_typeof(f->'bookingUrl') IS DISTINCT FROM 'string' OR
    jsonb_typeof(f->'note') IS DISTINCT FROM 'string' OR length(f->>'note')>2000 THEN RETURN false; END IF;
 IF jsonb_array_length(f->'suitability')>16 OR
    ((f->>'bookingUrl')<>'' AND NOT birdtie_business_https_valid(f->>'bookingUrl')) THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(f->'suitability') x WHERE jsonb_typeof(x) IS DISTINCT FROM 'string' OR length(x#>>'{}') NOT BETWEEN 1 AND 80 OR btrim(x#>>'{}') IS DISTINCT FROM x#>>'{}') OR
    (SELECT count(*) FROM jsonb_array_elements(f->'suitability'))<>(SELECT count(DISTINCT x) FROM jsonb_array_elements(f->'suitability') x) THEN RETURN false; END IF;
 RETURN true;
END $$;

CREATE TABLE business_review_grants (
 business_id uuid NOT NULL REFERENCES businesses(id),
 reviewer_account_id uuid NOT NULL REFERENCES accounts(id),
 permissions text[] NOT NULL CHECK(cardinality(permissions) BETWEEN 1 AND 3 AND permissions<@ARRAY['claim','profile','venue']::text[]),
 valid_from timestamptz NOT NULL,
 valid_until timestamptz NOT NULL CHECK(isfinite(valid_from) AND isfinite(valid_until) AND valid_until>valid_from AND valid_until<=valid_from+interval '366 days'),
 state text NOT NULL CHECK(state IN ('active','revoked')),
 provisioned_by uuid NOT NULL REFERENCES accounts(id),
 provision_note text NOT NULL CHECK(length(btrim(provision_note)) BETWEEN 1 AND 2000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(business_id,reviewer_account_id)
 ,CHECK(provisioned_by<>reviewer_account_id)
);
CREATE TABLE business_claim_controls (
 business_id uuid PRIMARY KEY REFERENCES businesses(id),
 version bigint NOT NULL CHECK(version>0),
 submitted_by uuid NOT NULL REFERENCES accounts(id),
 name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 160 AND name=btrim(name)),
 description text NOT NULL CHECK(length(description)<=3000),
 source_url text NOT NULL CHECK(birdtie_business_https_valid(source_url)),
 rights_note text NOT NULL CHECK(length(btrim(rights_note)) BETWEEN 1 AND 2000),
 state text NOT NULL CHECK(state IN ('pending','verified','rejected','revoked')),
 reviewed_by uuid REFERENCES accounts(id),
 reviewed_at timestamptz,
 review_note text NOT NULL DEFAULT '' CHECK(length(review_note)<=2000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((state='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note='') OR
       (state<>'pending' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND reviewed_by<>submitted_by AND length(btrim(review_note))>0))
);
CREATE TABLE business_console_profiles (
 business_id uuid PRIMARY KEY REFERENCES businesses(id),
 version bigint NOT NULL CHECK(version>0),
 submitted_by uuid NOT NULL REFERENCES accounts(id),
 facts jsonb NOT NULL CHECK(birdtie_business_profile_valid(facts)),
 source_url text NOT NULL CHECK(birdtie_business_https_valid(source_url)),
 rights_note text NOT NULL CHECK(length(btrim(rights_note)) BETWEEN 1 AND 2000),
 valid_until timestamptz NOT NULL,
 state text NOT NULL CHECK(state IN ('pending','verified','rejected','revoked')),
 reviewed_by uuid REFERENCES accounts(id),
 reviewed_at timestamptz,
 review_note text NOT NULL DEFAULT '' CHECK(length(review_note)<=2000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(isfinite(valid_until) AND isfinite(created_at) AND valid_until>created_at),
 CHECK((state='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note='') OR
       (state<>'pending' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND reviewed_by<>submitted_by AND length(btrim(review_note))>0))
);
CREATE TABLE business_console_venue_facts (
 business_id uuid NOT NULL REFERENCES businesses(id),
 place_id uuid NOT NULL REFERENCES venues(place_id),
 version bigint NOT NULL CHECK(version>0),
 submitted_by uuid NOT NULL REFERENCES accounts(id),
 facts jsonb NOT NULL CHECK(birdtie_business_venue_facts_valid(facts)),
 source_url text NOT NULL CHECK(birdtie_business_https_valid(source_url)),
 rights_note text NOT NULL CHECK(length(btrim(rights_note)) BETWEEN 1 AND 2000),
 valid_until timestamptz NOT NULL,
 state text NOT NULL CHECK(state IN ('pending','verified','rejected','revoked')),
 reviewed_by uuid REFERENCES accounts(id),
 reviewed_at timestamptz,
 review_note text NOT NULL DEFAULT '' CHECK(length(review_note)<=2000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(business_id,place_id),
 CHECK(isfinite(valid_until) AND isfinite(created_at) AND valid_until>created_at),
 CHECK((state='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note='') OR
       (state<>'pending' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND reviewed_by<>submitted_by AND length(btrim(review_note))>0))
);
CREATE TABLE business_console_membership_controls (
 business_id uuid PRIMARY KEY REFERENCES businesses(id),
 version bigint NOT NULL CHECK(version>0),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE business_console_audit_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 business_id uuid NOT NULL REFERENCES businesses(id),
 actor_account_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('claim_submit','claim_approve','claim_reject','claim_revoke','profile_submit','profile_approve','profile_reject','profile_revoke','venue_submit','venue_approve','venue_reject','venue_revoke','member_grant','member_remove','member_transfer_owner')),
 resource_id uuid NOT NULL,
 resource_version bigint NOT NULL CHECK(resource_version>0),
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(business_id,action,resource_id,resource_version)
);
CREATE INDEX business_console_audit_by_business ON business_console_audit_events(business_id,occurred_at,id);

-- These side rows must always bind an actual Person, not a Business/Org login.
CREATE FUNCTION birdtie_business_console_person_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE person uuid; reviewed uuid;
BEGIN
 IF TG_TABLE_NAME='business_review_grants' THEN
  person:=NEW.reviewer_account_id;reviewed:=NEW.provisioned_by;
  IF cardinality(NEW.permissions)<>(SELECT count(DISTINCT p) FROM unnest(NEW.permissions) p) THEN RAISE EXCEPTION 'distinct review permissions required'; END IF;
  IF TG_OP='UPDATE' AND (NEW.business_id IS DISTINCT FROM OLD.business_id OR NEW.reviewer_account_id IS DISTINCT FROM OLD.reviewer_account_id OR NEW.provisioned_by IS DISTINCT FROM OLD.provisioned_by OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN RAISE EXCEPTION 'stable review grant bindings required'; END IF;
 ELSIF TG_TABLE_NAME='business_console_audit_events' THEN
  person:=NEW.actor_account_id;
 ELSE person:=NEW.submitted_by;reviewed:=NEW.reviewed_by; END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=person AND account_type='person') OR
    (reviewed IS NOT NULL AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=reviewed AND account_type='person')) THEN
  RAISE EXCEPTION 'business console requires Person bindings';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER business_review_grants_person BEFORE INSERT OR UPDATE ON business_review_grants FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_person_guard();
CREATE TRIGGER business_claim_controls_person BEFORE INSERT OR UPDATE ON business_claim_controls FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_person_guard();
CREATE TRIGGER business_console_profiles_person BEFORE INSERT OR UPDATE ON business_console_profiles FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_person_guard();
CREATE TRIGGER business_console_venue_person BEFORE INSERT OR UPDATE ON business_console_venue_facts FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_person_guard();
CREATE TRIGGER business_console_audit_person BEFORE INSERT OR UPDATE ON business_console_audit_events FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_person_guard();
CREATE FUNCTION birdtie_business_console_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 THEN RAISE EXCEPTION 'business console starts at version one'; END IF;
 ELSE
  IF NEW.business_id IS DISTINCT FROM OLD.business_id OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'business console CAS required'; END IF;
  -- Keep row-type-specific fields inside a separate branch. PostgreSQL may
  -- resolve record fields while preparing an expression before boolean AND
  -- can short-circuit; non-Venue rows have no place_id at all.
  IF TG_TABLE_NAME='business_console_venue_facts' THEN
   IF NEW.place_id IS DISTINCT FROM OLD.place_id THEN RAISE EXCEPTION 'stable business venue required'; END IF;
  END IF;
  IF TG_TABLE_NAME<>'business_console_membership_controls' THEN
   IF NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION 'stable business source creation required'; END IF;
  END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER business_claim_controls_version BEFORE INSERT OR UPDATE ON business_claim_controls FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_version_guard();
CREATE TRIGGER business_console_profiles_version BEFORE INSERT OR UPDATE ON business_console_profiles FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_version_guard();
CREATE TRIGGER business_console_venue_version BEFORE INSERT OR UPDATE ON business_console_venue_facts FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_version_guard();
CREATE TRIGGER business_console_membership_version BEFORE INSERT OR UPDATE ON business_console_membership_controls FOR EACH ROW EXECUTE FUNCTION birdtie_business_console_version_guard();
COMMIT;
