BEGIN;
DROP INDEX IF EXISTS inbox_items_target_conversation;
ALTER TABLE inbox_items DROP COLUMN IF EXISTS target_conversation_id;
COMMIT;
