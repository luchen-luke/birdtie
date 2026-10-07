-- Explicit public semantic declarations. No backfill, private signals or model grant.
CREATE FUNCTION birdtie_place_semantic_facts_valid(f jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE k text; v jsonb; n numeric; lo numeric; hi numeric; any_known boolean:=false;
BEGIN
 IF jsonb_typeof(f)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(f))<>7
 OR NOT f ?& ARRAY['vibe','good_for','price','accessibility','group_size','reservation','suitability'] THEN RETURN false; END IF;
 FOREACH k IN ARRAY ARRAY['vibe','good_for','suitability'] LOOP
  v:=f->k;
  IF v='null'::jsonb THEN CONTINUE; END IF;
  IF jsonb_typeof(v)<>'array' OR jsonb_array_length(v)>16 THEN RETURN false; END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(v) e WHERE jsonb_typeof(e)<>'string' OR e#>>'{}' !~ '^[a-z][a-z0-9_]{1,39}$')
  OR (SELECT count(*) FROM jsonb_array_elements(v))<>(SELECT count(DISTINCT e) FROM jsonb_array_elements(v) e) THEN RETURN false; END IF;
  any_known:=any_known OR jsonb_array_length(v)>0;
 END LOOP;
 v:=f->'price';
 IF v<>'null'::jsonb THEN
  IF jsonb_typeof(v)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v))<>4 OR NOT v ?& ARRAY['currency','minMinor','maxMinor','unit']
  OR jsonb_typeof(v->'currency')<>'string' OR v->>'currency' !~ '^[A-Z]{3}$'
  OR jsonb_typeof(v->'unit')<>'string' OR v->>'unit' NOT IN ('per_person','per_visit','per_hour')
  OR jsonb_typeof(v->'minMinor')<>'number' OR jsonb_typeof(v->'maxMinor')<>'number'
  OR v->>'minMinor' !~ '^(0|[1-9][0-9]{0,8})$' OR v->>'maxMinor' !~ '^(0|[1-9][0-9]{0,8})$' THEN RETURN false; END IF;
  lo:=(v->>'minMinor')::numeric;hi:=(v->>'maxMinor')::numeric;
  IF lo>hi OR hi>100000000 THEN RETURN false; END IF;any_known:=true;
 END IF;
 v:=f->'accessibility';
 IF v<>'null'::jsonb THEN
  IF jsonb_typeof(v)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v))<>2 OR NOT v ?& ARRAY['stepFree','accessibleToilet']
  OR jsonb_typeof(v->'stepFree')<>'string' OR jsonb_typeof(v->'accessibleToilet')<>'string'
  OR v->>'stepFree' NOT IN ('yes','no','unknown') OR v->>'accessibleToilet' NOT IN ('yes','no','unknown')
  OR (v->>'stepFree'='unknown' AND v->>'accessibleToilet'='unknown') THEN RETURN false; END IF;any_known:=true;
 END IF;
 v:=f->'group_size';
 IF v<>'null'::jsonb THEN
  IF jsonb_typeof(v)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v))<>2 OR NOT v ?& ARRAY['min','max']
  OR jsonb_typeof(v->'min')<>'number' OR jsonb_typeof(v->'max')<>'number'
  OR v->>'min' !~ '^[1-9][0-9]{0,3}$' OR v->>'max' !~ '^[1-9][0-9]{0,3}$' THEN RETURN false; END IF;
  lo:=(v->>'min')::numeric;hi:=(v->>'max')::numeric;IF lo>hi OR hi>1000 THEN RETURN false; END IF;any_known:=true;
 END IF;
 v:=f->'reservation';
 IF v<>'null'::jsonb THEN
  IF jsonb_typeof(v)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v))<>2 OR NOT v ?& ARRAY['support','url']
  OR jsonb_typeof(v->'support')<>'string' OR v->>'support' NOT IN ('none','contact','external_url') THEN RETURN false; END IF;
  IF v->>'support'='external_url' THEN
   IF jsonb_typeof(v->'url')<>'string' OR length(v->>'url')>1000 OR v->>'url' !~ '^https://[^[:space:]@#]+$' THEN RETURN false; END IF;
  ELSIF v->'url'<>'null'::jsonb THEN RETURN false; END IF;any_known:=true;
 END IF;
 RETURN any_known AND octet_length(f::text)<=16384;
EXCEPTION WHEN numeric_value_out_of_range OR invalid_text_representation THEN RETURN false;
END $$;

CREATE TABLE place_semantic_candidates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 place_id uuid NOT NULL, city_id text NOT NULL,
 submitted_by uuid NOT NULL REFERENCES accounts(id), operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 submitter_generation text NOT NULL, editor_generation text NOT NULL,
 place_generation text NOT NULL, city_generation text NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 1000000000),
 version bigint NOT NULL DEFAULT 1 CHECK(version=1),
 facts jsonb NOT NULL CHECK(birdtie_place_semantic_facts_valid(facts)),
 source_label text NOT NULL CHECK(length(source_label) BETWEEN 2 AND 100 AND source_label=trim(source_label) AND source_label !~ '[[:cntrl:]]'),
 source_url text NOT NULL CHECK(length(source_url)<=1000 AND source_url ~ '^https://[^[:space:]@#]+$'),
 rights_note text NOT NULL CHECK(length(rights_note) BETWEEN 10 AND 1000 AND rights_note=trim(rights_note) AND rights_note !~ '[[:cntrl:]]'),
 observed_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 confidence_kind text NOT NULL CHECK(confidence_kind='EDITOR_ASSESSMENT_UNCALIBRATED'),
 confidence_level text NOT NULL CHECK(confidence_level IN ('LOW','MEDIUM','HIGH')),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
 reviewed_by uuid REFERENCES accounts(id), reviewed_at timestamptz, review_note text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(place_id,city_id) REFERENCES places(id,city_id),
 UNIQUE(city_id,submitted_by,operation_id),
 CHECK(observed_at<=created_at AND expires_at>created_at AND expires_at<=created_at+interval '365 days'),
 CHECK((status='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note IS NULL)
 OR (status IN ('approved','rejected') AND reviewed_by IS NOT NULL AND reviewed_by<>submitted_by
 AND reviewed_at IS NOT NULL AND reviewed_at>=created_at AND length(review_note) BETWEEN 10 AND 1000 AND review_note !~ '[[:cntrl:]]'))
);
CREATE INDEX place_semantic_candidates_review ON place_semantic_candidates(city_id,created_at,id) WHERE status='pending';

CREATE TABLE place_semantic_profiles (
 place_id uuid PRIMARY KEY, city_id text NOT NULL,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 1000000000),
 source_candidate_id uuid NOT NULL UNIQUE REFERENCES place_semantic_candidates(id),
 state text NOT NULL CHECK(state IN ('published','withdrawn')),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 withdrawn_by uuid REFERENCES accounts(id), withdraw_note text,
 FOREIGN KEY(place_id,city_id) REFERENCES places(id,city_id),
 CHECK((state='published' AND withdrawn_by IS NULL AND withdraw_note IS NULL)
 OR (state='withdrawn' AND withdrawn_by IS NOT NULL AND length(withdraw_note) BETWEEN 10 AND 1000 AND withdraw_note !~ '[[:cntrl:]]'))
);

CREATE FUNCTION birdtie_place_semantic_candidate_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.place_id,NEW.city_id,NEW.submitted_by,NEW.operation_id,NEW.request_hash,NEW.submitter_generation,NEW.editor_generation,
 NEW.place_generation,NEW.city_generation,NEW.expected_version,NEW.version,NEW.facts,NEW.source_label,NEW.source_url,NEW.rights_note,
 NEW.observed_at,NEW.expires_at,NEW.confidence_kind,NEW.confidence_level,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.place_id,OLD.city_id,OLD.submitted_by,OLD.operation_id,OLD.request_hash,OLD.submitter_generation,OLD.editor_generation,
 OLD.place_generation,OLD.city_generation,OLD.expected_version,OLD.version,OLD.facts,OLD.source_label,OLD.source_url,OLD.rights_note,
 OLD.observed_at,OLD.expires_at,OLD.confidence_kind,OLD.confidence_level,OLD.created_at)
 OR (OLD.status<>'pending' AND ROW(NEW.status,NEW.reviewed_by,NEW.reviewed_at,NEW.review_note) IS DISTINCT FROM ROW(OLD.status,OLD.reviewed_by,OLD.reviewed_at,OLD.review_note)) THEN
 RAISE EXCEPTION 'place_semantic_candidate_immutable' USING ERRCODE='23514'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER place_semantic_candidate_immutable BEFORE UPDATE ON place_semantic_candidates FOR EACH ROW EXECUTE FUNCTION birdtie_place_semantic_candidate_immutable();

CREATE FUNCTION birdtie_place_semantic_profile_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM place_semantic_candidates c WHERE c.id=NEW.source_candidate_id AND c.place_id=NEW.place_id AND c.city_id=NEW.city_id
 AND c.status='approved' AND c.reviewed_at<=NEW.updated_at AND c.expected_version=NEW.version-1)
 AND NOT (TG_OP='UPDATE' AND NEW.source_candidate_id=OLD.source_candidate_id AND NEW.version=OLD.version+1 AND NEW.state='withdrawn') THEN
 RAISE EXCEPTION 'place_semantic_profile_source' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND (NEW.place_id<>OLD.place_id OR NEW.city_id<>OLD.city_id OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at) THEN
 RAISE EXCEPTION 'place_semantic_profile_cas' USING ERRCODE='23514'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER place_semantic_profile_source BEFORE INSERT OR UPDATE ON place_semantic_profiles FOR EACH ROW EXECUTE FUNCTION birdtie_place_semantic_profile_source();
