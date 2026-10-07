BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM native_notification_policies) OR EXISTS(SELECT 1 FROM native_notification_decisions)
       OR EXISTS(SELECT 1 FROM inbox_items WHERE routing_decision_id IS NOT NULL OR resource_type IN
          ('community_message','activity_message','organization_membership','agent_task')) THEN
        RAISE EXCEPTION 'nonempty notification controls cannot be downgraded';
    END IF;
END $$;
DROP FUNCTION birdtie_native_notification_visible(native_notification_decisions);
DROP TRIGGER native_notification_decision_guard ON native_notification_decisions;
DROP FUNCTION birdtie_native_notification_decision_guard();
DROP FUNCTION birdtie_native_notification_preference(uuid,text);
DROP FUNCTION birdtie_native_notification_source(text,uuid,uuid);
DROP FUNCTION birdtie_native_notification_activity_chat_allowed(uuid,uuid);
ALTER TABLE inbox_items DROP COLUMN routing_decision_id;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN (
    'place_candidate','activity_candidate','community','connection_request','conversation_message',
    'activity_change','activity_cancelled','activity_reminder'));
DROP TABLE native_notification_decisions;
DROP TRIGGER native_notification_policy_guard ON native_notification_policies;
DROP FUNCTION birdtie_native_notification_policy_guard();
DROP TABLE native_notification_policies;
COMMIT;
