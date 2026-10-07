BEGIN;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS target_conversation_id uuid
    REFERENCES conversations(id) ON DELETE SET NULL;
UPDATE inbox_items i SET target_conversation_id=m.conversation_id
FROM conversation_messages m
WHERE i.resource_type='conversation_message' AND i.resource_id=m.id
  AND i.target_conversation_id IS NULL;
CREATE INDEX IF NOT EXISTS inbox_items_target_conversation
    ON inbox_items (target_conversation_id)
    WHERE target_conversation_id IS NOT NULL;
COMMIT;
