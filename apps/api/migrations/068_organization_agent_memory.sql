BEGIN;
-- The native ledger remains the only Memory content store. Existing Person
-- rows/IDs/versions/source contracts are untouched. Direct administrator
-- declarations neither certify external facts nor grant a cognitive purpose.
CREATE FUNCTION birdtie_org_memory_value_valid(value jsonb) RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT AS $$
BEGIN
 IF NOT birdtie_agent_memory_value_valid(value) THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM jsonb_object_keys(value) k WHERE k NOT IN ('note','tags')) THEN RETURN false; END IF;
 IF value ? 'note' AND jsonb_typeof(value->'note') IS DISTINCT FROM 'string' THEN RETURN false; END IF;
 IF value ? 'tags' THEN
  IF jsonb_typeof(value->'tags') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
  IF jsonb_array_length(value->'tags')>16 THEN RETURN false; END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(value->'tags') t WHERE jsonb_typeof(t) IS DISTINCT FROM 'string' OR octet_length(t#>>'{}') NOT BETWEEN 1 AND 80) THEN RETURN false; END IF;
 END IF;
 RETURN true;
END $$;
ALTER TABLE agent_memories DROP CONSTRAINT agent_memories_owner_type_check;
ALTER TABLE agent_memories ADD CONSTRAINT agent_memories_owner_type_check CHECK(owner_type IN ('PERSON','ORGANIZATION'));
ALTER TABLE agent_memories ADD CONSTRAINT agent_memory_organization_shape CHECK(owner_type<>'ORGANIZATION' OR (
 source_type='EXPLICIT' AND confidence=1 AND last_reinforced_at IS NULL AND status IN ('ACTIVE','EXPIRED','DELETED')
 AND visibility IN ('PRIVATE','AGENT_ONLY') AND birdtie_org_memory_value_valid(structured_value) AND (
(memory_type='ACTIVITY' AND memory_key ~ '^org[.]v1[.]past_activity[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='ACTIVITY' AND memory_key ~ '^org[.]v1[.]upcoming_activity[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='PLACE' AND memory_key ~ '^org[.]v1[.]venue[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='ORGANIZATION' AND memory_key ~ '^org[.]v1[.]announcement[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='ORGANIZATION' AND memory_key ~ '^org[.]v1[.]faq[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='ORGANIZATION' AND memory_key ~ '^org[.]v1[.]partner[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='COMMUNITY' AND memory_key ~ '^org[.]v1[.]community_relationship[.][a-z0-9][a-z0-9._:-]{0,59}$') OR
(memory_type='ORGANIZATION' AND memory_key ~ '^org[.]v1[.]policy[.][a-z0-9][a-z0-9._:-]{0,59}$'))));
-- 057 Evidence/063 candidate stay Person-only. 060's empty control row also
-- needs the old Person boundary when the common Memory owner type expands.
CREATE OR REPLACE FUNCTION birdtie_guard_memory_reinforcement() RETURNS trigger LANGUAGE plpgsql AS $$
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
 IF NOT EXISTS(SELECT 1 FROM agent_memories WHERE id=NEW.memory_id AND owner_type='PERSON' AND version=NEW.memory_version FOR SHARE) THEN RAISE EXCEPTION 'current Memory required'; END IF;
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

ALTER TABLE admin_audit_events DROP CONSTRAINT admin_audit_events_resource_type_check;
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_events_resource_type_check CHECK(resource_type IN ('organization','activity','faq','membership','organization_memory'));
ALTER TABLE admin_audit_events DROP CONSTRAINT admin_audit_events_action_check;
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_events_action_check CHECK(action IN ('organization_create','organization_profile_update','activity_create','activity_update','activity_publish','activity_cancel','faq_create','faq_update','faq_delete','membership_invite','membership_accept','membership_role_change','membership_revoke','organization_memory_put','organization_memory_delete'));
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_organization_memory_pair CHECK((resource_type='organization_memory')=(action IN ('organization_memory_put','organization_memory_delete')));
COMMIT;
