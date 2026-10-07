BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM community_conversations) OR EXISTS(SELECT 1 FROM incident_reports WHERE target_type='community_message') THEN
        RAISE EXCEPTION 'cannot remove community conversation data or reports';
    END IF;
END $$;
ALTER TABLE incident_reports DROP CONSTRAINT incident_reports_target_type_check;
ALTER TABLE incident_reports ADD CONSTRAINT incident_reports_target_type_check
    CHECK(target_type IN ('activity','organization','account','message','activity_message','community','business','general'));
DROP TRIGGER community_chat_membership_revoke ON community_memberships;
DROP FUNCTION birdtie_community_chat_membership_revoke();
DROP TABLE community_conversation_messages,community_conversation_members,community_conversations;
COMMIT;
