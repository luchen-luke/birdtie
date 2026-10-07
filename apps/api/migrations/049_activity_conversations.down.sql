BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM activity_conversations) OR EXISTS(SELECT 1 FROM incident_reports WHERE target_type='activity_message') THEN
        RAISE EXCEPTION 'cannot remove activity conversation data or reports';
    END IF;
END $$;
ALTER TABLE incident_reports DROP CONSTRAINT incident_reports_target_type_check;
ALTER TABLE incident_reports ADD CONSTRAINT incident_reports_target_type_check
    CHECK(target_type IN ('activity','organization','account','message','community','business','general'));
DROP TRIGGER activity_chat_rsvp_revoke ON activity_participations;
DROP FUNCTION birdtie_activity_chat_rsvp_revoke();
DROP TABLE activity_conversation_messages,activity_conversation_members,activity_conversations;
COMMIT;
