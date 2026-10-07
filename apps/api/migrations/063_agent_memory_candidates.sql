BEGIN;
-- Manual owner hypothesis storage. This schema grants no analysis/model rights.
CREATE TABLE agent_memory_candidates (
 id uuid PRIMARY KEY,
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 version bigint NOT NULL CHECK(version>0),
 metadata_version bigint NOT NULL CHECK(metadata_version>0),
 status text NOT NULL CHECK(status IN('CANDIDATE','ACTIVE','REJECTED','SUPERSEDED','EXPIRED')),
 predicate text, category text, assessment jsonb, sources jsonb NOT NULL,
 intent_digest text NOT NULL CHECK(intent_digest ~ '^[0-9a-f]{64}$'),
 decision_digest text CHECK(decision_digest IS NULL OR decision_digest ~ '^[0-9a-f]{64}$'),
 memory_id uuid REFERENCES agent_memories(id) ON DELETE CASCADE,
 memory_version bigint CHECK(memory_version>0),
 valid_until timestamptz NOT NULL, created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 UNIQUE(agent_id,intent_digest),
 CHECK(isfinite(valid_until) AND isfinite(created_at) AND isfinite(updated_at) AND created_at>='0001-01-01+00'::timestamptz AND valid_until<'10000-01-01+00'::timestamptz AND updated_at<'10000-01-01+00'::timestamptz AND valid_until>created_at AND valid_until<=created_at+interval '24 hours' AND updated_at>=created_at),
 CHECK(coalesce((status IN('CANDIDATE','ACTIVE') AND predicate='ACTIVITY_CATEGORY' AND category IN('badminton','basketball','football','sports','culture') AND jsonb_typeof(assessment)='object' AND jsonb_typeof(sources)='array' AND jsonb_array_length(sources) BETWEEN 1 AND 20) OR (status IN('REJECTED','SUPERSEDED','EXPIRED') AND predicate IS NULL AND category IS NULL AND assessment IS NULL AND sources='[]'::jsonb),false)),
 CHECK((status='ACTIVE' AND memory_id IS NOT NULL AND memory_version IS NOT NULL AND decision_digest IS NOT NULL) OR (status<>'ACTIVE' AND memory_id IS NULL AND memory_version IS NULL AND decision_digest IS NULL))
);
CREATE FUNCTION birdtie_guard_memory_candidate() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s jsonb; stamp timestamptz; seen text[]:='{}'; address text;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.status<>'CANDIDATE' THEN RAISE EXCEPTION 'candidate starts pending'; END IF;
 ELSE
  IF OLD.status<>'CANDIDATE' AND NOT(OLD.status='ACTIVE' AND NEW.status IN('SUPERSEDED','EXPIRED')) THEN RAISE EXCEPTION 'terminal candidate'; END IF;
  IF NEW.id<>OLD.id OR NEW.agent_id<>OLD.agent_id OR NEW.owner_id<>OLD.owner_id OR NEW.metadata_version<>OLD.metadata_version OR NEW.intent_digest<>OLD.intent_digest OR NEW.valid_until<>OLD.valid_until OR NEW.created_at<>OLD.created_at OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.status=OLD.status THEN RAISE EXCEPTION 'candidate immutable binding and CAS'; END IF;
  IF NEW.status='ACTIVE' AND (NEW.predicate IS DISTINCT FROM OLD.predicate OR NEW.category IS DISTINCT FROM OLD.category OR NEW.assessment IS DISTINCT FROM OLD.assessment OR NEW.sources IS DISTINCT FROM OLD.sources) THEN RAISE EXCEPTION 'reviewed content immutable'; END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=NEW.agent_id AND agent_type='personal' AND principal_account_id=NEW.owner_id) THEN RAISE EXCEPTION 'exact personal owner'; END IF;
 IF NEW.status IN('CANDIDATE','ACTIVE') THEN
  IF NOT coalesce((SELECT count(*)=2 FROM jsonb_object_keys(NEW.assessment)),false) OR
   NOT coalesce(((NEW.assessment->>'semantics'='ORDINAL' AND NEW.assessment->>'level' IN('LOW','MEDIUM','HIGH') AND NEW.assessment ? 'level') OR
    (NEW.assessment->>'semantics'='UNCALIBRATED_SCORE' AND jsonb_typeof(NEW.assessment->'value')='number' AND (NEW.assessment->>'value')::numeric BETWEEN 0 AND 1)),false) THEN RAISE EXCEPTION 'uncalibrated assessment required'; END IF;
  FOR s IN SELECT value FROM jsonb_array_elements(NEW.sources) LOOP
   IF NOT coalesce(jsonb_typeof(s)='object' AND (SELECT count(*) FROM jsonb_object_keys(s))=5 AND s ?& ARRAY['selector','version','eventTime','fingerprint','anchors'] AND jsonb_typeof(s->'selector')='object' AND (SELECT count(*) FROM jsonb_object_keys(s->'selector'))=2 AND s->'selector' ?& ARRAY['type','id'] AND (s->'selector'->>'id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' AND (s->>'fingerprint') ~ '^[0-9a-f]{64}$' AND jsonb_typeof(s->'version')='object' AND (SELECT count(*) FROM jsonb_object_keys(s->'version'))=2 AND jsonb_typeof(s->'eventTime')='string' AND jsonb_typeof(s->'anchors')='array' AND jsonb_array_length(s->'anchors') BETWEEN 1 AND 100,false) THEN RAISE EXCEPTION 'closed source metadata required'; END IF;
   IF NOT coalesce((s->'selector'->>'type'='MOMENT' AND s->'version'->>'kind'='REVISION' AND jsonb_typeof(s->'version'->'revision')='number' AND s->'version'->>'revision' ~ '^[1-9][0-9]*$') OR
     (s->'selector'->>'type'='ACTIVITY_PARTICIPATION' AND s->'version'->>'kind'='UPDATED_AT_DIGEST' AND s->'version'->>'token' ~ '^[0-9a-f]{64}$') OR
     (s->'selector'->>'type'='SAVED_PLACE' AND s->'version'->>'kind'='CREATED_AT_DIGEST' AND s->'version'->>'token' ~ '^[0-9a-f]{64}$'),false) THEN RAISE EXCEPTION 'native source version kind required'; END IF;
   stamp:=(s->>'eventTime')::timestamptz;
   IF NOT isfinite(stamp) OR stamp<'0001-01-01+00'::timestamptz OR stamp>NEW.created_at THEN RAISE EXCEPTION 'finite observed source time'; END IF;
   IF EXISTS(SELECT 1 FROM jsonb_array_elements(s->'anchors') a WHERE jsonb_typeof(a)<>'string' OR (a#>>'{}') !~ '^(ACTIVITY|MOMENT|PLACE):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') OR (SELECT count(*) FROM jsonb_array_elements(s->'anchors'))<>(SELECT count(DISTINCT a) FROM jsonb_array_elements(s->'anchors') a) THEN RAISE EXCEPTION 'native distinct anchors required'; END IF;
   address:=s->'selector'->>'type'||':'||(s->'selector'->>'id');IF address=ANY(seen) THEN RAISE EXCEPTION 'duplicate source';END IF;seen:=array_append(seen,address);
  END LOOP;
 END IF;
 IF NEW.status='ACTIVE' AND NOT EXISTS(SELECT 1 FROM agent_memories WHERE id=NEW.memory_id AND agent_id=NEW.agent_id AND owner_id=NEW.owner_id AND version=NEW.memory_version AND source_type='EXPLICIT' AND status='ACTIVE' AND valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'current declaration required'; END IF;
 NEW.updated_at:=GREATEST(clock_timestamp(),NEW.created_at);
 RETURN NEW;
END $$;
CREATE TRIGGER memory_candidate_guard BEFORE INSERT OR UPDATE ON agent_memory_candidates FOR EACH ROW EXECUTE FUNCTION birdtie_guard_memory_candidate();
CREATE FUNCTION birdtie_supersede_memory_candidates() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='agent_memories' THEN
 UPDATE agent_memory_candidates SET version=version+1,status='SUPERSEDED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL WHERE memory_id=NEW.id AND status='ACTIVE' AND memory_version<>NEW.version;
 ELSE
 UPDATE agent_memory_candidates c SET version=version+1,status='SUPERSEDED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL
 WHERE c.memory_id=NEW.memory_id AND c.status='ACTIVE' AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.sources) s WHERE s->'selector'->>'type'=OLD.source_type AND s->'selector'->>'id'=OLD.source_id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER memory_candidate_memory_changed AFTER UPDATE OF version ON agent_memories FOR EACH ROW WHEN(OLD.version IS DISTINCT FROM NEW.version) EXECUTE FUNCTION birdtie_supersede_memory_candidates();
CREATE TRIGGER memory_candidate_evidence_removed AFTER UPDATE OF status ON agent_memory_evidence FOR EACH ROW WHEN(OLD.status='CURRENT' AND NEW.status='REMOVED') EXECUTE FUNCTION birdtie_supersede_memory_candidates();
COMMIT;

