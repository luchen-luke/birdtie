BEGIN;

ALTER TABLE conversation_messages
    ADD COLUMN entity_type text,
    ADD COLUMN entity_id uuid;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_message_entity_pair CHECK (
    (entity_type IS NULL AND entity_id IS NULL) OR
    (entity_type IN ('activity','place','person','community','organization','business','moment') AND entity_id IS NOT NULL)
);
CREATE INDEX conversation_messages_entity ON conversation_messages(entity_type, entity_id)
    WHERE entity_type IS NOT NULL;

COMMIT;
