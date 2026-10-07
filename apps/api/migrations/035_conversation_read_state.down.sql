BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM conversation_member_states WHERE last_read_message_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove Chat read state while user cursors exist';
    END IF;
END $$;
DROP TRIGGER conversation_member_states_create ON conversations;
DROP FUNCTION birdtie_create_conversation_member_states();
DROP TRIGGER conversation_member_state_validate ON conversation_member_states;
DROP FUNCTION birdtie_validate_conversation_member_state();
DROP TABLE conversation_member_states;
DROP INDEX conversation_messages_conversation_id_id;
COMMIT;
