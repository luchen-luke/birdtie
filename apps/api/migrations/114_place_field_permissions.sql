BEGIN;
-- Existing candidate/source rows keep NULL permissions. No old rights note is
-- upgraded to confirmed rights and no existing record is deleted or rewritten.
CREATE TABLE IF NOT EXISTS place_source_permissions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 candidate_id uuid NOT NULL UNIQUE REFERENCES place_candidates(id) DEFERRABLE INITIALLY DEFERRED,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 state text NOT NULL CHECK(state IN('CONFIRMED','PENDING','PROHIBITED')),
 acquisition text NOT NULL CHECK(acquisition IN('FIRST_PARTY','DIRECT_AUTHORIZATION','OPEN_LICENSE')),
 origin_provider text NOT NULL CHECK(origin_provider IN('BIRDTIE_FIRST_PARTY','DIRECT_SOURCE','OPEN_DATA_DOWNLOAD')),
 source_url text NOT NULL CHECK(source_url ~ '^https://[^/@[:space:]]+[.][^/@[:space:]]+'),
 license_url text NOT NULL CHECK(license_url ~ '^https://[^/@[:space:]]+[.][^/@[:space:]]+'),
 license_version text NOT NULL CHECK(length(license_version) BETWEEN 1 AND 160),
 attribution text NOT NULL CHECK(length(attribution) BETWEEN 1 AND 500),
 acquired_at timestamptz NOT NULL CHECK(isfinite(acquired_at)),
 retention_until timestamptz NOT NULL CHECK(isfinite(retention_until) AND retention_until>acquired_at),
 declared_by uuid NOT NULL REFERENCES accounts(id),
 reviewed_by uuid REFERENCES accounts(id), reviewed_at timestamptz,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((reviewed_by IS NULL)=(reviewed_at IS NULL)),
 CHECK(reviewed_by IS NULL OR reviewed_by<>declared_by),
 CHECK((acquisition='FIRST_PARTY' AND origin_provider='BIRDTIE_FIRST_PARTY') OR (acquisition='DIRECT_AUTHORIZATION' AND origin_provider='DIRECT_SOURCE') OR (acquisition='OPEN_LICENSE' AND origin_provider IN('DIRECT_SOURCE','OPEN_DATA_DOWNLOAD')))
);
ALTER TABLE place_candidates ADD COLUMN IF NOT EXISTS permission_id uuid REFERENCES place_source_permissions(id);
ALTER TABLE place_candidates ADD COLUMN IF NOT EXISTS field_claims jsonb;
ALTER TABLE place_sources ADD COLUMN IF NOT EXISTS permission_id uuid REFERENCES place_source_permissions(id);

CREATE TABLE IF NOT EXISTS place_field_evidence (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),place_id uuid NOT NULL REFERENCES places(id),
 source_candidate_id uuid NOT NULL REFERENCES place_candidates(id),
 field_key text NOT NULL CHECK(field_key IN('name','address','coordinates','food_seafood','food_spicy','opening_hours')),
 value jsonb NOT NULL,
 observed_at timestamptz NOT NULL CHECK(isfinite(observed_at)),
 verified_at timestamptz NOT NULL CHECK(isfinite(verified_at)),
 valid_from timestamptz NOT NULL CHECK(isfinite(valid_from)),
 valid_to timestamptz NOT NULL CHECK(isfinite(valid_to) AND valid_to>valid_from),
 refresh_due timestamptz NOT NULL CHECK(isfinite(refresh_due) AND refresh_due>=valid_from AND refresh_due<=valid_to),
 permission_id uuid NOT NULL REFERENCES place_source_permissions(id),
 permission_version bigint NOT NULL CHECK(permission_version>0),version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 conflict_state text NOT NULL CHECK(conflict_state IN('NONE','UNRESOLVED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(place_id,field_key,permission_id),
 CHECK((field_key IN('name','address','opening_hours') AND jsonb_typeof(value)='string' AND length(value#>>'{}') BETWEEN 1 AND 1200) OR (field_key IN('food_seafood','food_spicy') AND jsonb_typeof(value)='boolean') OR (field_key='coordinates' AND jsonb_typeof(value)='object' AND value->>'coordinateSystem'='wgs84' AND jsonb_typeof(value->'latitude')='number' AND jsonb_typeof(value->'longitude')='number' AND (value->>'latitude')::numeric BETWEEN -90 AND 90 AND (value->>'longitude')::numeric BETWEEN -180 AND 180))
);
CREATE INDEX IF NOT EXISTS place_field_current_lookup ON place_field_evidence(place_id,field_key,valid_to,refresh_due);

CREATE OR REPLACE FUNCTION birdtie_guard_place_source_permission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'retained source permission cannot be deleted';END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.state<>'CONFIRMED' OR NEW.reviewed_by IS NOT NULL OR NEW.revoked_at IS NOT NULL OR NEW.acquired_at>clock_timestamp() OR NEW.retention_until<=clock_timestamp() THEN RAISE EXCEPTION 'explicit current rights declaration required';END IF;
 ELSE
  IF (to_jsonb(NEW)-ARRAY['version','state','reviewed_by','reviewed_at','revoked_at','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['version','state','reviewed_by','reviewed_at','revoked_at','updated_at']) THEN RAISE EXCEPTION 'source rights basis is immutable; submit a new candidate';END IF;
  IF NEW.state IS DISTINCT FROM OLD.state OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
   IF NEW.state NOT IN('PENDING','PROHIBITED') OR NEW.version<>OLD.version+1 OR NEW.revoked_at IS NULL THEN RAISE EXCEPTION 'permission revocation requires next version and timestamp';END IF;
  ELSIF NEW.version<>OLD.version THEN RAISE EXCEPTION 'unexplained permission version';END IF;
 END IF;
 IF NEW.state='CONFIRMED' AND NEW.reviewed_by IS NOT NULL AND (NEW.reviewed_by=NEW.declared_by OR NEW.reviewed_at>clock_timestamp() OR NOT EXISTS(SELECT 1 FROM place_candidates c JOIN city_editor_memberships m ON m.city_id=c.city_id JOIN accounts a ON a.id=m.account_id WHERE c.id=NEW.candidate_id AND c.submitted_by=NEW.declared_by AND m.account_id=NEW.reviewed_by AND m.role='reviewer' AND m.state='active' AND a.status='active')) THEN RAISE EXCEPTION 'independent current city reviewer required';END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS place_source_permission_guard ON place_source_permissions;
CREATE TRIGGER place_source_permission_guard BEFORE INSERT OR UPDATE OR DELETE ON place_source_permissions FOR EACH ROW EXECUTE FUNCTION birdtie_guard_place_source_permission();

CREATE OR REPLACE FUNCTION birdtie_guard_place_candidate_permission() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p place_source_permissions;
BEGIN
 IF TG_OP='INSERT' OR (NEW.status IN('published','linked_duplicate') AND NEW.status IS DISTINCT FROM OLD.status) THEN
  SELECT * INTO p FROM place_source_permissions WHERE id=NEW.permission_id;
  IF NOT FOUND OR p.candidate_id<>NEW.id OR p.declared_by<>NEW.submitted_by OR p.source_url<>NEW.source_url OR p.state<>'CONFIRMED' OR p.revoked_at IS NOT NULL OR p.retention_until<NEW.expires_at OR p.retention_until<=clock_timestamp() OR NEW.field_claims IS NULL OR jsonb_typeof(NEW.field_claims)<>'array' OR jsonb_array_length(NEW.field_claims) NOT BETWEEN 1 AND 6 THEN RAISE EXCEPTION 'current retained source permission and fields required';END IF;
  IF regexp_replace(lower(coalesce(NEW.provider_code,'')),'[._[:space:]-]','','g') ~ '(tencent|searchpro|^wsa$)' OR lower(NEW.source_url) ~ '^https://(tokenhub[.]tencentmaas[.]com|[^/]+[.]tencentmaas[.]com|wsa[.]tencentcloudapi[.]com|searchpro[.]tencentcloudapi[.]com)([:/]|$)' THEN RAISE EXCEPTION 'restricted search origin cannot enter city catalog';END IF;
  IF NEW.status IN('published','linked_duplicate') AND (p.reviewed_by IS DISTINCT FROM NEW.reviewed_by OR p.reviewed_at IS NULL OR p.reviewed_by=NEW.submitted_by) THEN RAISE EXCEPTION 'rights review required before publication';END IF;
 END IF;
 IF TG_OP='UPDATE' AND OLD.permission_id IS NOT NULL AND (NEW.permission_id IS DISTINCT FROM OLD.permission_id OR NEW.field_claims IS DISTINCT FROM OLD.field_claims OR NEW.source_url IS DISTINCT FROM OLD.source_url OR NEW.provider_code IS DISTINCT FROM OLD.provider_code OR NEW.name IS DISTINCT FROM OLD.name OR NEW.address_label IS DISTINCT FROM OLD.address_label OR NEW.latitude IS DISTINCT FROM OLD.latitude OR NEW.longitude IS DISTINCT FROM OLD.longitude) THEN RAISE EXCEPTION 'retained candidate facts are immutable';END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS place_candidate_permission_guard ON place_candidates;
CREATE TRIGGER place_candidate_permission_guard BEFORE INSERT OR UPDATE ON place_candidates FOR EACH ROW EXECUTE FUNCTION birdtie_guard_place_candidate_permission();

CREATE OR REPLACE FUNCTION birdtie_guard_place_field_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p place_source_permissions;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'retained field evidence cannot be deleted';END IF;
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'source field evidence is immutable; submit reviewed new evidence';END IF;
 SELECT * INTO p FROM place_source_permissions WHERE id=NEW.permission_id;
 IF NOT FOUND OR p.candidate_id<>NEW.source_candidate_id OR p.version<>NEW.permission_version OR p.state<>'CONFIRMED' OR p.revoked_at IS NOT NULL OR p.reviewed_by IS NULL OR p.reviewed_by=p.declared_by OR NEW.valid_to>p.retention_until OR NEW.valid_to<=clock_timestamp() OR NEW.observed_at>clock_timestamp() OR NEW.verified_at>clock_timestamp() OR NOT EXISTS(SELECT 1 FROM place_candidates c WHERE c.id=NEW.source_candidate_id AND c.permission_id=p.id) THEN RAISE EXCEPTION 'independently reviewed current source field required';END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS place_field_evidence_guard ON place_field_evidence;
CREATE TRIGGER place_field_evidence_guard BEFORE INSERT OR UPDATE OR DELETE ON place_field_evidence FOR EACH ROW EXECUTE FUNCTION birdtie_guard_place_field_evidence();
COMMIT;
