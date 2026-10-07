BEGIN;
-- The original immutable message is the effect. No separate effect ledger.
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_message_share_operation_shape;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_message_share_operation_shape
 CHECK (client_operation_id IS NULL OR
  (entity_type IS NOT NULL AND entity_id IS NOT NULL) OR
  (entity_type IS NULL AND entity_id IS NULL AND speaker_kind='human' AND octet_length(body) BETWEEN 1 AND 2000));
CREATE UNIQUE INDEX conversation_message_plain_operation
 ON conversation_messages(sender_account_id,client_operation_id)
 WHERE client_operation_id IS NOT NULL AND entity_type IS NULL AND entity_id IS NULL;
COMMIT;
