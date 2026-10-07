BEGIN;
-- A tombstone or audit is still retained control data. Refuse atomically;
-- never discard a declaration or history to force a rollback.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_memories WHERE owner_type='ORGANIZATION') OR EXISTS(SELECT 1 FROM admin_audit_events WHERE resource_type='organization_memory' OR action IN ('organization_memory_put','organization_memory_delete')) THEN
  RAISE EXCEPTION 'organization Memory or audit retained; rollback refused';
 END IF;
END $$;
ALTER TABLE admin_audit_events DROP CONSTRAINT admin_audit_organization_memory_pair;
ALTER TABLE admin_audit_events DROP CONSTRAINT admin_audit_events_resource_type_check;
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_events_resource_type_check CHECK(resource_type IN ('organization','activity','faq','membership'));
ALTER TABLE admin_audit_events DROP CONSTRAINT admin_audit_events_action_check;
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_events_action_check CHECK(action IN ('organization_create','organization_profile_update','activity_create','activity_update','activity_publish','activity_cancel','faq_create','faq_update','faq_delete','membership_invite','membership_accept','membership_role_change','membership_revoke'));
ALTER TABLE agent_memories DROP CONSTRAINT agent_memory_organization_shape;
ALTER TABLE agent_memories DROP CONSTRAINT agent_memories_owner_type_check;
ALTER TABLE agent_memories ADD CONSTRAINT agent_memories_owner_type_check CHECK(owner_type='PERSON');
DROP FUNCTION birdtie_org_memory_value_valid(jsonb);
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
COMMIT;
