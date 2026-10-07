BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM conversation_messages WHERE client_operation_id IS NOT NULL AND entity_type IS NULL)
 THEN RAISE EXCEPTION 'plain message operation history exists; refusing down' USING ERRCODE='P0001'; END IF;
END $$;
DROP INDEX conversation_message_plain_operation;
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_message_share_operation_shape;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_message_share_operation_shape
 CHECK (client_operation_id IS NULL OR (entity_type IS NOT NULL AND entity_id IS NOT NULL));
COMMIT;
