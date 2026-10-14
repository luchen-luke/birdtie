BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM human_entity_share_operations) THEN RAISE EXCEPTION 'entity share operation history exists; downgrade refused' USING ERRCODE='23514';END IF;END $$;
DROP TRIGGER entity_share_late_message_guard ON conversation_messages;
DROP FUNCTION birdtie_entity_share_late_message_guard();
DROP TABLE human_entity_share_operations;
DROP FUNCTION birdtie_entity_share_operation_guard();
COMMIT;
