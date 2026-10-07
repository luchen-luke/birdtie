BEGIN;
-- Human operations and negative decisions only; no probability or new reader grant.
CREATE TABLE agent_memory_corrections(
 id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'),
 owner_id uuid NOT NULL,agent_id uuid NOT NULL,owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),session_id uuid NOT NULL,
 target_kind text NOT NULL CHECK(target_kind IN('MEMORY','CANDIDATE')),target_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 9223372036854775806),
 action text NOT NULL CHECK(action IN('EDIT','DELETE','REJECT','NEGATE')),
 category text CHECK(category IN('badminton','basketball','football','sports','culture','hiking')),
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object' AND octet_length(input::text)<=20000),
 authority text NOT NULL CHECK(authority~'^[0-9a-f]{64}$'),binding text NOT NULL CHECK(binding~'^[0-9a-f]{64}$'),
 plan_digest text NOT NULL CHECK(plan_digest~'^[0-9a-f]{64}$'),result_id uuid NOT NULL,memory_until timestamptz,
 observed_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 committed_at timestamptz,result_memory_id uuid,result_memory_version bigint,
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
 CHECK(isfinite(observed_at) AND isfinite(expires_at) AND observed_at>='0001-01-01+00' AND expires_at<'10000-01-01+00' AND expires_at>observed_at AND expires_at<=observed_at+interval '5 minutes'),
 CHECK((action='NEGATE')=(category IS NOT NULL)),
 CHECK(action NOT IN('EDIT','DELETE') OR target_kind='MEMORY'),
 CHECK((action='NEGATE' AND memory_until IS NOT NULL AND isfinite(memory_until) AND memory_until=observed_at+interval '365 days') OR(action<>'NEGATE' AND memory_until IS NULL)),
 CHECK((committed_at IS NULL AND result_memory_id IS NULL AND result_memory_version IS NULL) OR
 (committed_at IS NOT NULL AND isfinite(committed_at) AND committed_at>=observed_at AND committed_at<expires_at AND
 ((result_memory_id IS NULL AND result_memory_version IS NULL) OR(result_memory_id IS NOT NULL AND result_memory_version>0))))
);
CREATE TABLE agent_memory_suppressions(
 agent_id uuid NOT NULL,owner_id uuid NOT NULL,owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 predicate text NOT NULL CHECK(predicate='ACTIVITY_CATEGORY'),category text NOT NULL CHECK(category IN('badminton','basketball','football','sports','culture','hiking')),
 operation_id uuid NOT NULL REFERENCES agent_memory_corrections(id),created_at timestamptz NOT NULL CHECK(isfinite(created_at)),
 PRIMARY KEY(agent_id,predicate,category),
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE
);
-- Append-only source invalidation metadata. The source trigger never locks a
-- Candidate/Memory: its original source mutation lock order remains unchanged.
CREATE TABLE agent_memory_source_invalidations(
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 source_type text NOT NULL CHECK(source_type IN('MOMENT','ACTIVITY_PARTICIPATION','SAVED_PLACE')),source_id uuid NOT NULL,
 source_epoch text NOT NULL CHECK(length(source_epoch) BETWEEN 1 AND 100),created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at))
);
CREATE INDEX memory_source_invalidations_current ON agent_memory_source_invalidations(owner_id,source_type,source_id,id DESC);
CREATE FUNCTION birdtie_memory_source_invalidation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=OLD.owner_id) THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'source invalidation metadata retained' USING ERRCODE='23514';
END $$;
CREATE TRIGGER memory_source_invalidation_guard BEFORE UPDATE OR DELETE ON agent_memory_source_invalidations FOR EACH ROW EXECUTE FUNCTION birdtie_memory_source_invalidation_guard();
CREATE FUNCTION birdtie_memory_correction_authority(o uuid,a uuid,sid uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('owner',to_jsonb(ac),'ownerRow',ac.xmin::text,'agent',to_jsonb(ag),'agentRow',ag.xmin::text,
 'metadata',to_jsonb(ap),'metadataRow',ap.xmin::text,'session',se.id,'created',se.created_at,'method',se.authentication_method,'absoluteExpiry',se.expires_at,'digest',encode(se.token_sha256,'hex'))::text,'UTF8')),'hex')
 FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=ac.id AND ap.owner_type='PERSON' JOIN sessions se ON se.account_id=ac.id
 WHERE ac.id=o AND ag.id=a AND se.id=sid AND ac.account_type='person' AND ac.status='active' AND ag.agent_type='personal' AND ag.status='active'
 AND se.revoked_at IS NULL AND isfinite(se.expires_at) AND isfinite(se.idle_expires_at) AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()
$$;
CREATE FUNCTION birdtie_memory_correction_binding(o uuid,a uuid,k text,t uuid,cat text) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object(
 'memories',(SELECT coalesce(jsonb_agg(jsonb_build_object('row',to_jsonb(m),'epoch',m.xmin::text) ORDER BY m.id),'[]') FROM agent_memories m WHERE m.owner_id=o AND m.agent_id=a AND m.owner_type='PERSON' AND((k='MEMORY' AND m.id=t) OR(cat IS NOT NULL AND m.memory_type='PREFERENCE' AND m.memory_key='activity_category:'||cat AND m.status IN('ACTIVE','PENDING_REVIEW')))),
 'candidates',(SELECT coalesce(jsonb_agg(jsonb_build_object('row',to_jsonb(c),'epoch',c.xmin::text) ORDER BY c.id),'[]') FROM agent_memory_candidates c WHERE c.owner_id=o AND c.agent_id=a AND((k='CANDIDATE' AND c.id=t) OR(cat IS NOT NULL AND c.predicate='ACTIVITY_CATEGORY' AND c.category=cat AND c.status IN('CANDIDATE','ACTIVE')))),
 'suppression',(SELECT to_jsonb(s) FROM agent_memory_suppressions s WHERE s.owner_id=o AND s.agent_id=a AND s.predicate='ACTIVITY_CATEGORY' AND s.category=cat)
 )::text,'UTF8')),'hex')
$$;
CREATE FUNCTION birdtie_memory_correction_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n timestamptz:=clock_timestamp();m agent_memories%ROWTYPE;replacement jsonb;
BEGIN
 IF TG_OP='DELETE' THEN
  IF EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=OLD.agent_id AND owner_id=OLD.owner_id) THEN RAISE EXCEPTION 'correction history retained' USING ERRCODE='23514'; END IF;RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
  -- Expired private review contents may be scrubbed, never renewed or approved.
  IF OLD.committed_at IS NULL AND OLD.expires_at<=n AND NEW.input='{}'::jsonb AND OLD.input<>'{}'::jsonb AND
   (to_jsonb(NEW)-'input') IS NOT DISTINCT FROM (to_jsonb(OLD)-'input') THEN RETURN NEW; END IF;
  IF OLD.committed_at IS NOT NULL OR NEW.committed_at IS NULL OR
   (to_jsonb(NEW)-ARRAY['committed_at','result_memory_id','result_memory_version','input']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['committed_at','result_memory_id','result_memory_version','input']) OR NEW.input<>'{}'::jsonb THEN RAISE EXCEPTION 'immutable correction and once receipt' USING ERRCODE='23514'; END IF;
 ELSE
  IF NEW.committed_at IS NOT NULL OR NEW.observed_at>n OR NEW.input->>'id' IS DISTINCT FROM NEW.id::text OR NEW.input->>'targetKind' IS DISTINCT FROM NEW.target_kind OR NEW.input->>'targetId' IS DISTINCT FROM NEW.target_id::text OR NEW.input->>'expectedVersion' IS DISTINCT FROM NEW.expected_version::text OR NEW.input->>'action' IS DISTINCT FROM NEW.action OR NEW.input->>'category' IS DISTINCT FROM NEW.category OR
   NOT NEW.input ?& ARRAY['id','targetKind','targetId','expectedVersion','action'] OR
   EXISTS(SELECT 1 FROM jsonb_object_keys(NEW.input) k WHERE k<>ALL(ARRAY['id','targetKind','targetId','expectedVersion','action','category','replacement'])) OR
   (NEW.action='EDIT')<>(NEW.input ? 'replacement') OR (NEW.action='NEGATE')<>(NEW.input ? 'category') THEN RAISE EXCEPTION 'closed original correction input' USING ERRCODE='23514'; END IF;
  IF NEW.action='EDIT' AND (jsonb_typeof(NEW.input->'replacement') IS DISTINCT FROM 'object' OR NOT (NEW.input->'replacement') ?& ARRAY['expectedVersion','memoryType','memoryKey','summary','structuredValue','visibility','validUntil'] OR (SELECT count(*) FROM jsonb_object_keys(NEW.input->'replacement'))<>7 OR NEW.input->'replacement'->>'expectedVersion' IS DISTINCT FROM NEW.expected_version::text) THEN RAISE EXCEPTION 'closed replacement input' USING ERRCODE='23514';END IF;
  IF NEW.binding IS DISTINCT FROM birdtie_memory_correction_binding(NEW.owner_id,NEW.agent_id,NEW.target_kind,NEW.target_id,NEW.category) THEN RAISE EXCEPTION 'exact original targets' USING ERRCODE='23514';END IF;
 END IF;
 IF NEW.authority IS DISTINCT FROM birdtie_memory_correction_authority(NEW.owner_id,NEW.agent_id,NEW.session_id) OR NEW.expires_at<=n THEN RAISE EXCEPTION 'current correction authority and deadline' USING ERRCODE='23514';END IF;
 IF TG_OP='INSERT' THEN
  IF NOT((NEW.target_kind='MEMORY' AND EXISTS(SELECT 1 FROM agent_memories WHERE id=NEW.target_id AND owner_id=NEW.owner_id AND agent_id=NEW.agent_id AND version=NEW.expected_version AND status<>'DELETED' AND isfinite(valid_from) AND isfinite(valid_until) AND isfinite(updated_at) AND valid_from<=n AND valid_until>n AND (NEW.action<>'EDIT' OR source_type='EXPLICIT') AND (NEW.action<>'NEGATE' OR(memory_type='PREFERENCE' AND memory_key='activity_category:'||NEW.category)))) OR(NEW.target_kind='CANDIDATE' AND EXISTS(SELECT 1 FROM agent_memory_candidates WHERE id=NEW.target_id AND owner_id=NEW.owner_id AND agent_id=NEW.agent_id AND version=NEW.expected_version AND status='CANDIDATE' AND isfinite(valid_until) AND isfinite(updated_at) AND valid_until>n AND (NEW.action<>'NEGATE' OR category=NEW.category)))) THEN RAISE EXCEPTION 'current target required' USING ERRCODE='23514';END IF;
 ELSE
  IF NEW.target_kind='CANDIDATE' AND NEW.action='REJECT' THEN
   IF NEW.result_memory_id IS NOT NULL OR NOT EXISTS(SELECT 1 FROM agent_memory_candidates WHERE id=NEW.target_id AND owner_id=NEW.owner_id AND agent_id=NEW.agent_id AND version=NEW.expected_version+1 AND status='REJECTED') THEN RAISE EXCEPTION 'actual rejected candidate required' USING ERRCODE='23514';END IF;
  ELSE
   SELECT * INTO m FROM agent_memories WHERE id=NEW.result_memory_id AND owner_id=NEW.owner_id AND agent_id=NEW.agent_id AND version=NEW.result_memory_version;
   IF NOT FOUND OR (NEW.action<>'NEGATE' AND (m.id<>NEW.target_id OR m.version<>NEW.expected_version+1)) THEN RAISE EXCEPTION 'actual exact memory result required' USING ERRCODE='23514';END IF;
   IF NEW.action IN('DELETE','REJECT') AND (m.status<>'DELETED' OR m.summary<>'' OR m.structured_value<>'{}'::jsonb) THEN RAISE EXCEPTION 'scrubbed discarded memory required' USING ERRCODE='23514';END IF;
   IF NEW.action IN('EDIT','NEGATE') AND (m.status<>'ACTIVE' OR m.source_type<>'EXPLICIT' OR m.confidence<>1 OR NOT isfinite(m.valid_until) OR m.valid_until<=n) THEN RAISE EXCEPTION 'current human declaration required' USING ERRCODE='23514';END IF;
   IF NEW.action='EDIT' THEN
    replacement:=OLD.input->'replacement';
    IF m.memory_type IS DISTINCT FROM replacement->>'memoryType' OR m.memory_key IS DISTINCT FROM replacement->>'memoryKey' OR m.summary IS DISTINCT FROM replacement->>'summary' OR m.structured_value IS DISTINCT FROM replacement->'structuredValue' OR m.visibility IS DISTINCT FROM replacement->>'visibility' OR m.valid_until IS DISTINCT FROM (replacement->>'validUntil')::timestamptz THEN RAISE EXCEPTION 'reviewed replacement required' USING ERRCODE='23514';END IF;
   ELSIF NEW.action='NEGATE' THEN
    IF m.memory_type<>'PREFERENCE' OR m.memory_key<>'activity_category:'||NEW.category OR m.visibility<>'PRIVATE' OR m.valid_until<>NEW.memory_until OR m.structured_value<>jsonb_build_object('activityCategory',NEW.category,'nature','human-correction') OR m.summary<>(CASE NEW.category WHEN 'badminton' THEN '我不偏好羽毛球活动' WHEN 'basketball' THEN '我不偏好篮球活动' WHEN 'football' THEN '我不偏好足球活动' WHEN 'sports' THEN '我不偏好运动活动' WHEN 'culture' THEN '我不偏好文化活动' WHEN 'hiking' THEN '我不偏好徒步活动' END) THEN RAISE EXCEPTION 'concrete negative human declaration required' USING ERRCODE='23514';END IF;
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER memory_correction_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_memory_corrections FOR EACH ROW EXECUTE FUNCTION birdtie_memory_correction_guard();
CREATE FUNCTION birdtie_memory_suppression_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN
  IF TG_OP='DELETE' AND NOT EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=OLD.agent_id AND owner_id=OLD.owner_id) THEN RETURN OLD;END IF;
  RAISE EXCEPTION 'suppression is a retained human decision';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM agent_memory_corrections c WHERE c.id=NEW.operation_id AND c.owner_id=NEW.owner_id AND c.agent_id=NEW.agent_id AND c.action='NEGATE' AND c.category=NEW.category AND c.committed_at IS NOT NULL AND c.committed_at=NEW.created_at) THEN RAISE EXCEPTION 'committed concrete human negative decision required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER memory_suppression_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_memory_suppressions FOR EACH ROW EXECUTE FUNCTION birdtie_memory_suppression_guard();
CREATE FUNCTION birdtie_memory_candidate_correction_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN('CANDIDATE','ACTIVE') AND EXISTS(SELECT 1 FROM agent_memory_suppressions WHERE agent_id=NEW.agent_id AND owner_id=NEW.owner_id AND predicate=NEW.predicate AND category=NEW.category) THEN RAISE EXCEPTION 'explicit human correction suppresses proposal' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE TRIGGER memory_candidate_correction_guard BEFORE INSERT OR UPDATE ON agent_memory_candidates FOR EACH ROW EXECUTE FUNCTION birdtie_memory_candidate_correction_guard();
CREATE FUNCTION birdtie_memory_source_invalidated() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o uuid;kind text;epoch text;
BEGIN
 IF TG_TABLE_NAME='moments' THEN o:=OLD.author_account_id;kind:='MOMENT';epoch:=OLD.revision::text;
 ELSIF TG_TABLE_NAME='activity_participations' THEN o:=OLD.participant_account_id;kind:='ACTIVITY_PARTICIPATION';epoch:=OLD.updated_at::text;
 ELSE o:=OLD.owner_account_id;kind:='SAVED_PLACE';epoch:=OLD.created_at::text;END IF;
 IF EXISTS(SELECT 1 FROM accounts WHERE id=o) THEN INSERT INTO agent_memory_source_invalidations(owner_id,source_type,source_id,source_epoch) VALUES(o,kind,OLD.id,epoch);END IF;
 IF TG_OP='DELETE' THEN RETURN OLD;END IF;RETURN NEW;
END $$;
CREATE TRIGGER memory_moment_invalidated AFTER UPDATE OF revision,status,visibility OR DELETE ON moments FOR EACH ROW EXECUTE FUNCTION birdtie_memory_source_invalidated();
CREATE TRIGGER memory_participation_invalidated AFTER UPDATE OR DELETE ON activity_participations FOR EACH ROW EXECUTE FUNCTION birdtie_memory_source_invalidated();
CREATE TRIGGER memory_saved_invalidated AFTER UPDATE OR DELETE ON saved_items FOR EACH ROW EXECUTE FUNCTION birdtie_memory_source_invalidated();
COMMIT;
