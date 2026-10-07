BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM conversation_messages WHERE entity_type IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove Chat entity cards while shared messages exist';
    END IF;
END $$;
DROP INDEX conversation_messages_entity;
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_message_entity_pair;
ALTER TABLE conversation_messages DROP COLUMN entity_id, DROP COLUMN entity_type;
COMMIT;
