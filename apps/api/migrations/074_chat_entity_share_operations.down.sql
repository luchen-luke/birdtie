BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM conversation_messages WHERE client_operation_id IS NOT NULL)
 THEN RAISE EXCEPTION 'cannot discard persisted entity share recovery keys' USING ERRCODE='55000'; END IF;
END $$;
DROP TRIGGER conversation_message_share_receipt_immutable ON conversation_messages;
DROP FUNCTION birdtie_immutable_entity_share_receipt();
DROP INDEX conversation_message_share_operation;
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_message_share_operation_shape;
ALTER TABLE conversation_messages DROP COLUMN client_operation_id;
COMMIT;
