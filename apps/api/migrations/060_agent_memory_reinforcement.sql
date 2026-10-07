BEGIN;
-- Human reinforcement has its own CAS. Existing Memory content/version and
-- Evidence bindings are untouched; source access remains the service's job.
CREATE TABLE agent_memory_reinforcement (
 memory_id uuid PRIMARY KEY REFERENCES agent_memories(id) ON DELETE CASCADE,
 memory_version bigint NOT NULL CHECK(memory_version>0),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 entries jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(entries)='array' AND jsonb_array_length(entries)<=100),
 last_plan_digest text CHECK(last_plan_digest IS NULL OR last_plan_digest ~ '^[0-9a-f]{64}$'),
 last_support_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at AND created_at>='0001-01-01+00'::timestamptz AND updated_at<'10000-01-01+00'::timestamptz),
 CHECK((entries='[]'::jsonb AND last_support_at IS NULL AND last_plan_digest IS NULL) OR
       (entries<>'[]'::jsonb AND last_support_at IS NOT NULL AND isfinite(last_support_at) AND last_support_at>=created_at AND last_support_at<=updated_at AND last_plan_digest IS NOT NULL))
);
CREATE FUNCTION birdtie_guard_memory_reinforcement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e jsonb; seen uuid[]:='{}'; mid uuid;
BEGIN
 IF TG_OP='DELETE' THEN
  IF EXISTS(SELECT 1 FROM agent_memories WHERE id=OLD.memory_id) THEN RAISE EXCEPTION 'reinforcement requires versioned clear'; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 THEN RAISE EXCEPTION 'reinforcement starts at one'; END IF;
 ELSE
  IF NEW.memory_id IS DISTINCT FROM OLD.memory_id OR NEW.created_at IS DISTINCT FROM OLD.created_at OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'reinforcement CAS required'; END IF;
 END IF;
 NEW.updated_at:=GREATEST(clock_timestamp(),NEW.created_at);
 IF NOT EXISTS(SELECT 1 FROM agent_memories WHERE id=NEW.memory_id AND version=NEW.memory_version FOR SHARE) THEN RAISE EXCEPTION 'current Memory required'; END IF;
 IF NEW.entries<>'[]'::jsonb THEN
  IF NOT EXISTS(SELECT 1 FROM agent_memories WHERE id=NEW.memory_id AND source_type='EXPLICIT' AND status='ACTIVE' AND valid_from<=clock_timestamp() AND valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'active declaration required'; END IF;
  FOR e IN SELECT value FROM jsonb_array_elements(NEW.entries) LOOP
   IF jsonb_typeof(e)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(e))<>4 OR
      NOT(e ?& ARRAY['evidenceId','evidenceVersion','fingerprint','anchors']) OR
      jsonb_typeof(e->'anchors')<>'array' OR jsonb_array_length(e->'anchors') NOT BETWEEN 1 AND 100 OR
      jsonb_typeof(e->'evidenceId') IS DISTINCT FROM 'string' OR
      jsonb_typeof(e->'evidenceVersion') IS DISTINCT FROM 'number' OR
      (e->>'evidenceVersion') !~ '^[1-9][0-9]*$' OR
      jsonb_typeof(e->'fingerprint') IS DISTINCT FROM 'string' OR
      (e->>'fingerprint') !~ '^[0-9a-f]{64}$' OR
      EXISTS(SELECT 1 FROM jsonb_array_elements(e->'anchors') a WHERE jsonb_typeof(a)<>'string' OR
        (a #>> '{}') !~ '^(ACTIVITY|MOMENT|PLACE):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') OR
      (SELECT count(*) FROM jsonb_array_elements(e->'anchors'))<>(SELECT count(DISTINCT a) FROM jsonb_array_elements(e->'anchors') a)
      THEN RAISE EXCEPTION 'invalid support shape'; END IF;
   mid:=(e->>'evidenceId')::uuid;
   IF mid=ANY(seen) OR NOT EXISTS(SELECT 1 FROM agent_memory_evidence WHERE id=mid AND memory_id=NEW.memory_id AND memory_version=NEW.memory_version AND status='CURRENT' AND version=(e->>'evidenceVersion')::bigint) THEN RAISE EXCEPTION 'current distinct Evidence required'; END IF;
   seen:=array_append(seen,mid);
  END LOOP;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER memory_reinforcement_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_memory_reinforcement FOR EACH ROW EXECUTE FUNCTION birdtie_guard_memory_reinforcement();
CREATE FUNCTION birdtie_clear_memory_reinforcement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Conservative whole-support clear prevents a changed declaration/detached
 -- Evidence from retaining its approval. Other sources require a new preview.
 IF TG_TABLE_NAME='agent_memories' THEN
  UPDATE agent_memory_reinforcement SET memory_version=NEW.version,version=version+1,entries='[]',last_plan_digest=NULL,last_support_at=NULL WHERE memory_id=NEW.id AND (memory_version<>NEW.version OR entries<>'[]'::jsonb);
 ELSE
  UPDATE agent_memory_reinforcement r SET memory_version=(SELECT version FROM agent_memories WHERE id=NEW.memory_id),version=r.version+1,entries='[]',last_plan_digest=NULL,last_support_at=NULL
   WHERE r.memory_id=NEW.memory_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.entries) e WHERE e->>'evidenceId'=NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER memory_reinforcement_memory_changed AFTER UPDATE OF version ON agent_memories FOR EACH ROW WHEN(OLD.version IS DISTINCT FROM NEW.version) EXECUTE FUNCTION birdtie_clear_memory_reinforcement();
CREATE TRIGGER memory_reinforcement_evidence_removed AFTER UPDATE OF status ON agent_memory_evidence FOR EACH ROW WHEN(OLD.status='CURRENT' AND NEW.status='REMOVED') EXECUTE FUNCTION birdtie_clear_memory_reinforcement();
COMMIT;
