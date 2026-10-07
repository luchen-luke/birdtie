BEGIN;

-- Original messages are the effect and recovery source; no shadow effect ledger.
ALTER TABLE conversation_messages ADD COLUMN client_operation_id uuid;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_message_share_operation_shape
 CHECK (client_operation_id IS NULL OR (entity_type IS NOT NULL AND entity_id IS NOT NULL));
CREATE UNIQUE INDEX conversation_message_share_operation
 ON conversation_messages(sender_account_id,conversation_id,client_operation_id)
 WHERE client_operation_id IS NOT NULL;

CREATE FUNCTION birdtie_immutable_entity_share_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.client_operation_id IS NOT NULL AND
  (NEW.client_operation_id,NEW.conversation_id,NEW.sender_account_id,NEW.body,NEW.entity_type,NEW.entity_id,NEW.id,NEW.created_at)
  IS DISTINCT FROM
  (OLD.client_operation_id,OLD.conversation_id,OLD.sender_account_id,OLD.body,OLD.entity_type,OLD.entity_id,OLD.id,OLD.created_at)
 THEN RAISE EXCEPTION 'entity share receipt is immutable' USING ERRCODE='55000'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER conversation_message_share_receipt_immutable BEFORE UPDATE ON conversation_messages
 FOR EACH ROW EXECUTE FUNCTION birdtie_immutable_entity_share_receipt();

COMMIT;
