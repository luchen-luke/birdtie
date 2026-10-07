BEGIN;

-- Human self-confirmation metadata only. No new Profile fields, machine grant,
-- source body copy, inference, Memory promotion or automatic expiration writer.
CREATE TABLE agent_profile_completion_previews (
 id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'::uuid),
 owner_id uuid NOT NULL,
 agent_id uuid NOT NULL,
 owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 session_id uuid NOT NULL,
 memory_id uuid NOT NULL,
 memory_version bigint NOT NULL CHECK(memory_version>0),
 expected_profile_version bigint NOT NULL CHECK(expected_profile_version BETWEEN 1 AND 9223372036854775806),
 category text NOT NULL CHECK(category IN ('badminton','basketball','football','sports','culture','hiking')),
 authority text NOT NULL CHECK(authority ~ '^[0-9a-f]{64}$'),
 source_binding text NOT NULL CHECK(source_binding ~ '^[0-9a-f]{64}$'),
 profile_binding text NOT NULL CHECK(profile_binding ~ '^[0-9a-f]{64}$'),
 previous_fields_digest text NOT NULL CHECK(previous_fields_digest ~ '^[0-9a-f]{64}$'),
 plan_digest text NOT NULL CHECK(plan_digest ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL CHECK(isfinite(observed_at)),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 result_profile_version bigint,
 committed_at timestamptz,
 result_profile_binding text,
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
 CHECK(expires_at>observed_at AND expires_at<=observed_at+interval '5 minutes'),
 CHECK((result_profile_version IS NULL AND committed_at IS NULL AND result_profile_binding IS NULL) OR
  (result_profile_version IS NOT NULL AND result_profile_version=expected_profile_version+1 AND committed_at IS NOT NULL AND isfinite(committed_at)
   AND committed_at>=observed_at AND committed_at<expires_at AND result_profile_binding IS NOT NULL AND result_profile_binding ~ '^[0-9a-f]{64}$'))
);
CREATE INDEX agent_profile_completion_previews_owner ON agent_profile_completion_previews(owner_id,observed_at);

CREATE FUNCTION birdtie_profile_completion_profile_binding(a uuid,o uuid) RETURNS text
LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('metadata',to_jsonb(ap),'metadataRow',ap.xmin::text,
  'private',to_jsonb(p),'privateRow',p.xmin::text)::text,'UTF8')),'hex')
 FROM agent_profiles ap LEFT JOIN agent_private_profiles p ON p.agent_id=ap.agent_id AND p.owner_id=ap.owner_id AND p.owner_type='PERSON'
 WHERE ap.agent_id=a AND ap.owner_id=o AND ap.owner_type='PERSON'
$$;
CREATE FUNCTION birdtie_profile_completion_authority(o uuid,a uuid,sid uuid) RETURNS text
LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('owner',to_jsonb(ac),'ownerRow',ac.xmin::text,
  'agent',to_jsonb(ag),'agentRow',ag.xmin::text,'session',se.id,'created',se.created_at,
  'method',se.authentication_method,'absoluteExpiry',se.expires_at,'digest',encode(se.token_sha256,'hex'))::text,'UTF8')),'hex')
 FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN sessions se ON se.account_id=ac.id
 WHERE ac.id=o AND ag.id=a AND se.id=sid
$$;
CREATE FUNCTION birdtie_profile_completion_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 value text;
 fields jsonb;
 current_version bigint;
BEGIN
 IF TG_OP='DELETE' THEN
  IF EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=OLD.agent_id AND owner_id=OLD.owner_id) THEN
   RAISE EXCEPTION 'profile completion history is retained' USING ERRCODE='23514';
  END IF;
  RETURN OLD;
 END IF;
 value:=CASE NEW.category WHEN 'badminton' THEN '我偏好羽毛球活动' WHEN 'basketball' THEN '我偏好篮球活动'
  WHEN 'football' THEN '我偏好足球活动' WHEN 'sports' THEN '我偏好运动活动' WHEN 'culture' THEN '我偏好文化活动'
  WHEN 'hiking' THEN '我偏好徒步活动' END;
 IF TG_OP='INSERT' AND (NEW.result_profile_version IS NOT NULL OR NEW.committed_at IS NOT NULL OR NEW.result_profile_binding IS NOT NULL) THEN
  RAISE EXCEPTION 'new profile completion is an uncommitted preview' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' THEN
  IF to_jsonb(NEW)-ARRAY['result_profile_version','committed_at','result_profile_binding']::text[] IS DISTINCT FROM
     to_jsonb(OLD)-ARRAY['result_profile_version','committed_at','result_profile_binding']::text[] OR
     OLD.result_profile_version IS NOT NULL OR NEW.result_profile_version IS DISTINCT FROM OLD.expected_profile_version+1 OR
     NEW.committed_at IS NULL OR NEW.result_profile_binding IS NULL THEN
   RAISE EXCEPTION 'profile completion tuple and terminal receipt are immutable' USING ERRCODE='23514';
  END IF;
 END IF;
 -- Locks resolve native mutations before the subsequent fresh clock checks.
 PERFORM 1 FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN sessions se ON se.account_id=ac.id
  JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=ac.id AND ap.owner_type='PERSON'
  JOIN agent_memories m ON m.agent_id=ag.id AND m.owner_id=ac.id AND m.owner_type='PERSON'
  WHERE ac.id=NEW.owner_id AND ag.id=NEW.agent_id AND se.id=NEW.session_id AND m.id=NEW.memory_id
  FOR SHARE OF ac,ag,se,ap,m;
 IF NOT EXISTS(SELECT 1 FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN sessions se ON se.account_id=ac.id
  JOIN agent_memories m ON m.agent_id=ag.id AND m.owner_id=ac.id AND m.owner_type='PERSON'
  WHERE ac.id=NEW.owner_id AND ac.account_type='person' AND ac.status='active'
   AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active'
   AND se.id=NEW.session_id AND se.revoked_at IS NULL AND isfinite(se.created_at) AND isfinite(se.expires_at) AND isfinite(se.idle_expires_at)
   AND se.created_at<=clock_timestamp() AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()
   AND m.id=NEW.memory_id AND m.version=NEW.memory_version AND m.memory_type='PREFERENCE' AND m.source_type='EXPLICIT'
   AND m.visibility='PRIVATE' AND m.status='ACTIVE' AND m.confidence=1 AND m.last_reinforced_at IS NULL
   AND m.memory_key='activity_category:'||NEW.category AND m.summary=value
   AND m.structured_value=jsonb_build_object('activityCategory',NEW.category,'nature','human-declaration')
   AND isfinite(m.created_at) AND isfinite(m.updated_at) AND isfinite(m.valid_from) AND isfinite(m.valid_until)
   AND m.created_at<=clock_timestamp() AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp() AND m.updated_at<=clock_timestamp()
   AND NEW.expires_at<=m.valid_until AND NEW.expires_at<=se.expires_at AND NEW.expires_at<=se.idle_expires_at
   AND encode(sha256(convert_to(jsonb_build_object('memory',to_jsonb(m),'row',m.xmin::text)::text,'UTF8')),'hex')=NEW.source_binding)
  OR NOT EXISTS(SELECT 1 FROM agent_profiles ap WHERE ap.agent_id=NEW.agent_id AND ap.owner_id=NEW.owner_id AND ap.owner_type='PERSON'
   AND isfinite(ap.created_at) AND isfinite(ap.updated_at) AND ap.created_at<=clock_timestamp() AND ap.updated_at<=clock_timestamp())
  OR EXISTS(SELECT 1 FROM agent_private_profiles p WHERE p.agent_id=NEW.agent_id AND
   (NOT isfinite(p.created_at) OR NOT isfinite(p.updated_at) OR p.created_at>clock_timestamp() OR p.updated_at>clock_timestamp()))
  OR NEW.expires_at<=clock_timestamp() OR
  birdtie_profile_completion_authority(NEW.owner_id,NEW.agent_id,NEW.session_id) IS DISTINCT FROM NEW.authority THEN
  RAISE EXCEPTION 'profile completion requires current exact human source' USING ERRCODE='23514';
 END IF;
 SELECT ap.profile_version,p.fields INTO current_version,fields FROM agent_profiles ap
  LEFT JOIN agent_private_profiles p ON p.agent_id=ap.agent_id AND p.owner_id=ap.owner_id AND p.owner_type='PERSON'
  WHERE ap.agent_id=NEW.agent_id AND ap.owner_id=NEW.owner_id AND ap.owner_type='PERSON';
 fields:=coalesce(fields,'{"personalPreferences":[],"socialPreferences":[],"availability":"","preferredActivityTypes":[],"travelPreferences":[],"interactionPreferences":[],"privateCityHistory":"","languagePreferences":[],"agentNotes":""}'::jsonb);
 IF TG_OP='INSERT' THEN
  IF current_version IS DISTINCT FROM NEW.expected_profile_version OR fields->'preferredActivityTypes' IS DISTINCT FROM '[]'::jsonb
   OR birdtie_profile_completion_profile_binding(NEW.agent_id,NEW.owner_id) IS DISTINCT FROM NEW.profile_binding
   OR encode(sha256(convert_to(fields::text,'UTF8')),'hex') IS DISTINCT FROM NEW.previous_fields_digest THEN
   RAISE EXCEPTION 'profile completion requires exact empty target and complete profile' USING ERRCODE='23514';
  END IF;
 ELSE
  IF current_version IS DISTINCT FROM NEW.result_profile_version OR fields->'preferredActivityTypes' IS DISTINCT FROM jsonb_build_array(value)
   OR birdtie_profile_completion_profile_binding(NEW.agent_id,NEW.owner_id) IS DISTINCT FROM NEW.result_profile_binding
   OR encode(sha256(convert_to(jsonb_set(fields,'{preferredActivityTypes}','[]'::jsonb)::text,'UTF8')),'hex') IS DISTINCT FROM NEW.previous_fields_digest THEN
   RAISE EXCEPTION 'profile completion receipt requires one exact native CAS' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_profile_completion_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_profile_completion_previews
 FOR EACH ROW EXECUTE FUNCTION birdtie_profile_completion_guard();
COMMIT;
