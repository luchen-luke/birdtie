BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM native_notification_decisions WHERE business_claim_id IS NOT NULL) OR EXISTS(SELECT 1 FROM inbox_items WHERE resource_type='business_claim_review') THEN RAISE EXCEPTION 'retained business review history forbids rollback' USING ERRCODE='55000'; END IF; END $$;
DROP FUNCTION birdtie_native_notification_source(text,uuid,uuid);
ALTER FUNCTION birdtie_native_notification_legacy_source_v079(text,uuid,uuid) RENAME TO birdtie_native_notification_source;
CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record; current_policy record; expected_cat text;
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
    expected_cat:=CASE WHEN NEW.kind='opportunity_available' THEN 'ACTIVITY' WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY'
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
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_business_claim_source_check;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK ((resource_type = ANY (ARRAY['place_candidate'::text, 'activity_candidate'::text, 'community'::text, 'connection_request'::text, 'conversation_message'::text, 'activity_change'::text, 'activity_cancelled'::text, 'activity_reminder'::text, 'community_message'::text, 'activity_message'::text, 'organization_membership'::text, 'agent_task'::text, 'opportunity_available'::text])));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check CHECK ((num_nonnulls(activity_id, activity_candidate_id, place_candidate_id, message_id, connection_request_id, community_message_id, activity_message_id, organization_membership_id, agent_task_id) = 1));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check1;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check1 CHECK ((source_id = COALESCE(activity_id, activity_candidate_id, place_candidate_id, message_id, connection_request_id, community_message_id, activity_message_id, organization_membership_id, agent_task_id)));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_kind_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_kind_check CHECK ((kind = ANY (ARRAY['activity_reminder'::text, 'activity_change'::text, 'activity_cancelled'::text, 'activity_review'::text, 'place_review'::text, 'direct_message'::text, 'connection_request'::text, 'connection_decision'::text, 'community_message'::text, 'activity_message'::text, 'organization_invitation'::text, 'organization_membership_change'::text, 'agent_task_completed'::text, 'agent_task_failed'::text, 'opportunity_available'::text])));
ALTER TABLE native_notification_decisions DROP COLUMN business_claim_id;
COMMIT;
