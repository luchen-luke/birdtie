BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM native_notification_decisions WHERE kind='opportunity_available') THEN RAISE EXCEPTION 'opportunity notifications retain current decisions; down refused' USING ERRCODE='55000';END IF;END $$;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN ('place_candidate','activity_candidate','community','connection_request','conversation_message','activity_change','activity_cancelled','activity_reminder','community_message','activity_message','organization_membership','agent_task'));
DO $$ DECLARE registry_name text; activity_name text; BEGIN
 SELECT conname INTO STRICT registry_name FROM pg_constraint WHERE conrelid='native_notification_decisions'::regclass AND contype='c' AND position('activity_reminder' in pg_get_constraintdef(oid))>0 AND position('activity_id' in pg_get_constraintdef(oid))=0;
 SELECT conname INTO STRICT activity_name FROM pg_constraint WHERE conrelid='native_notification_decisions'::regclass AND contype='c' AND position('activity_reminder' in pg_get_constraintdef(oid))>0 AND position('activity_id' in pg_get_constraintdef(oid))>0;
 EXECUTE format('ALTER TABLE native_notification_decisions DROP CONSTRAINT %I',registry_name);
 EXECUTE format('ALTER TABLE native_notification_decisions ADD CONSTRAINT %I CHECK(kind IN (''activity_reminder'',''activity_change'',''activity_cancelled'',''activity_review'',''place_review'',''direct_message'',''connection_request'',''connection_decision'',''community_message'',''activity_message'',''organization_invitation'',''organization_membership_change'',''agent_task_completed'',''agent_task_failed''))',registry_name);
 EXECUTE format('ALTER TABLE native_notification_decisions DROP CONSTRAINT %I',activity_name);
 EXECUTE format('ALTER TABLE native_notification_decisions ADD CONSTRAINT %I CHECK((kind IN (''activity_reminder'',''activity_change'',''activity_cancelled''))=(activity_id IS NOT NULL))',activity_name);
END $$;
DROP FUNCTION birdtie_native_notification_source(text,uuid,uuid);
ALTER FUNCTION birdtie_native_notification_legacy_source_v061(text,uuid,uuid) RENAME TO birdtie_native_notification_source;
CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record; current_policy record; expected_cat text;
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
    expected_cat:=CASE WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY'
        WHEN NEW.kind IN ('direct_message','activity_message') THEN 'MESSAGE'
        WHEN NEW.kind IN ('connection_request','connection_decision') THEN 'SOCIAL'
        WHEN NEW.kind='community_message' THEN 'COMMUNITY'
        WHEN NEW.kind LIKE 'organization_%' THEN 'ORGANIZATION'
        WHEN NEW.kind LIKE 'agent_task_%' THEN 'AGENT' WHEN NEW.kind='place_review' THEN 'SYSTEM' END;
    SELECT * INTO current_source FROM birdtie_native_notification_source(NEW.kind,NEW.source_id,NEW.recipient_id);
    IF NOT FOUND OR NEW.category IS DISTINCT FROM expected_cat OR NEW.actor_id IS DISTINCT FROM current_source.actor_id
       OR NEW.event_version IS DISTINCT FROM current_source.event_version OR NEW.source_version IS DISTINCT FROM current_source.source_version THEN
        RAISE EXCEPTION 'notification current source unavailable' USING ERRCODE='23514';
    END IF;
    SELECT * INTO current_policy FROM birdtie_native_notification_preference(NEW.recipient_id,NEW.category);
    IF NOT FOUND OR NEW.policy_version<>current_policy.version OR NEW.disposition<>current_policy.disposition
       OR NEW.priority<>current_policy.priority OR NEW.reason<>current_policy.reason THEN
        RAISE EXCEPTION 'notification policy changed' USING ERRCODE='23514';
    END IF;
    NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_native_notification_visible(d native_notification_decisions) RETURNS boolean LANGUAGE sql VOLATILE AS $$
    SELECT d.disposition IN ('IMMEDIATE','NORMAL') AND EXISTS(
        SELECT 1 FROM birdtie_native_notification_source(d.kind,d.source_id,d.recipient_id) s
        CROSS JOIN birdtie_native_notification_preference(d.recipient_id,d.category) p
        WHERE s.actor_id=d.actor_id AND s.event_version=d.event_version AND s.source_version=d.source_version
          AND p.version=d.policy_version AND p.disposition=d.disposition AND p.priority=d.priority AND p.reason=d.reason);
$$;
COMMIT;
