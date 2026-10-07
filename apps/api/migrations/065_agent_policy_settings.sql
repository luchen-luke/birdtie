BEGIN;

-- Human Person settings only. No default backfill, processing/source consent,
-- feature flag, model grant, autonomous action or old notification rewrite.
CREATE FUNCTION birdtie_agent_policy_settings_valid(family text, value jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE rule jsonb; seen text[]:='{}'; key text;
BEGIN
 IF jsonb_typeof(value)<>'object' OR octet_length(value::text)>8192 THEN RETURN false;END IF;
 IF family='ATTENTION' THEN
  IF value-ARRAY['defaultRoute','rules','pauseUntil']::text[]<>'{}'::jsonb
   OR jsonb_typeof(value->'defaultRoute') IS DISTINCT FROM 'string'
   OR value->>'defaultRoute' NOT IN ('IMMEDIATE','NORMAL','DIGEST','SILENT','BLOCK')
   OR jsonb_typeof(value->'rules') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  IF value?'pauseUntil' AND (jsonb_typeof(value->'pauseUntil') IS DISTINCT FROM 'string' OR
   (value->>'pauseUntil') !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?Z$') THEN RETURN false;END IF;
  IF jsonb_array_length(value->'rules')>13 THEN RETURN false;END IF;
  FOR rule IN SELECT jsonb_array_elements(value->'rules') LOOP
   IF jsonb_typeof(rule)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(rule))<>2
    OR rule-ARRAY['eventType','route']::text[]<>'{}'::jsonb
    OR jsonb_typeof(rule->'eventType') IS DISTINCT FROM 'string' OR jsonb_typeof(rule->'route') IS DISTINCT FROM 'string'
    OR rule->>'eventType' NOT IN ('MomentCreated','UserQuery','MomentUpdated','MomentDeleted','ActivityJoined','ActivityLeft','ActivityCompleted','PlaceSaved','PlaceVisited','CommunityJoined','CommunityLeft','ProfileUpdated','PreferenceUpdated')
    OR rule->>'route' NOT IN ('IMMEDIATE','NORMAL','DIGEST','SILENT','BLOCK') OR rule->>'eventType'=ANY(seen) THEN RETURN false;END IF;
   seen:=array_append(seen,rule->>'eventType');
  END LOOP;
 ELSIF family='SOCIAL' THEN
  IF value-ARRAY['rules']::text[]<>'{}'::jsonb OR jsonb_typeof(value->'rules') IS DISTINCT FROM 'array' OR jsonb_array_length(value->'rules')<>7 THEN RETURN false;END IF;
  FOR rule IN SELECT jsonb_array_elements(value->'rules') LOOP
   IF jsonb_typeof(rule)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(rule))<>2
    OR rule-ARRAY['category','preference']::text[]<>'{}'::jsonb
    OR jsonb_typeof(rule->'category') IS DISTINCT FROM 'string' OR jsonb_typeof(rule->'preference') IS DISTINCT FROM 'string'
    OR rule->>'category' NOT IN ('SAME_UNIVERSITY','SHARED_COMMUNITY','SHARED_ACTIVITY','EXISTING_CONNECTION','UNKNOWN_PERSON','BUSINESS','ORGANIZATION')
    OR rule->>'preference' NOT IN ('DISABLED','REVIEW_REQUIRED') OR rule->>'category'=ANY(seen) THEN RETURN false;END IF;
   seen:=array_append(seen,rule->>'category');
  END LOOP;
 ELSIF family='AUTONOMY' THEN
  IF (SELECT count(*) FROM jsonb_object_keys(value))<>1 OR jsonb_typeof(value->'level') IS DISTINCT FROM 'string'
    OR value->>'level' NOT IN ('LEVEL_0_OBSERVE','LEVEL_1_ASSIST','LEVEL_2_PREPARE') THEN RETURN false;END IF;
 ELSE RETURN false;
 END IF;
 RETURN true;
END $$;

CREATE TABLE agent_policy_settings (
 agent_id uuid NOT NULL,
 owner_type text NOT NULL CHECK(owner_type='PERSON'),
 owner_id uuid NOT NULL,
 family text NOT NULL CHECK(family IN ('ATTENTION','SOCIAL','AUTONOMY')),
 schema_version text NOT NULL CHECK(schema_version='agent-policy-settings-v1'),
 native_revision bigint NOT NULL CHECK(native_revision>0),
 settings jsonb NOT NULL CHECK(birdtie_agent_policy_settings_valid(family,settings)),
 valid_from timestamptz NOT NULL CHECK(isfinite(valid_from)),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 updated_at timestamptz NOT NULL CHECK(isfinite(updated_at)),
 PRIMARY KEY(agent_id,family),
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
 CHECK(updated_at=valid_from AND expires_at>valid_from AND expires_at<=valid_from+interval '720 hours')
);

CREATE FUNCTION birdtie_agent_policy_settings_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE pause timestamptz; current_clock timestamptz:=clock_timestamp();
BEGIN
 IF NOT EXISTS(SELECT 1 FROM agent_profiles ap JOIN accounts a ON a.id=ap.owner_id
  JOIN agents ag ON ag.id=ap.agent_id AND ag.principal_account_id=a.id
  WHERE ap.agent_id=NEW.agent_id AND ap.owner_id=NEW.owner_id AND ap.owner_type=NEW.owner_type
   AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND ag.status='active') THEN
  RAISE EXCEPTION 'policy owner unavailable' USING ERRCODE='23514';END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.native_revision<>1 THEN RAISE EXCEPTION 'policy initial version invalid' USING ERRCODE='23514';END IF;
 ELSE
  IF NEW.agent_id<>OLD.agent_id OR NEW.owner_id<>OLD.owner_id OR NEW.owner_type<>OLD.owner_type OR NEW.family<>OLD.family
   OR NEW.schema_version<>OLD.schema_version OR OLD.native_revision=9223372036854775807 OR NEW.native_revision<>OLD.native_revision+1 THEN
   RAISE EXCEPTION 'policy binding/version immutable' USING ERRCODE='23514';END IF;
 END IF;
 IF NEW.valid_from>current_clock OR NEW.expires_at<=current_clock THEN RAISE EXCEPTION 'policy time invalid' USING ERRCODE='23514';END IF;
 IF NEW.family='ATTENTION' AND NEW.settings?'pauseUntil' THEN
  BEGIN pause:=(NEW.settings->>'pauseUntil')::timestamptz;EXCEPTION WHEN OTHERS THEN RAISE EXCEPTION 'policy pause invalid' USING ERRCODE='23514';END;
  IF NOT isfinite(pause) OR pause<NEW.valid_from OR pause>NEW.expires_at THEN RAISE EXCEPTION 'policy pause invalid' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_policy_settings_guard BEFORE INSERT OR UPDATE ON agent_policy_settings FOR EACH ROW EXECUTE FUNCTION birdtie_agent_policy_settings_guard();
COMMIT;
