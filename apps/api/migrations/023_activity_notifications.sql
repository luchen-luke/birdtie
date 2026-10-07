BEGIN;
ALTER TABLE inbox_items
    ADD COLUMN IF NOT EXISTS target_activity_id uuid REFERENCES activities(id) ON DELETE SET NULL;
ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check
    CHECK (resource_type IN (
        'place_candidate', 'activity_candidate', 'community',
        'connection_request', 'conversation_message',
        'activity_change', 'activity_cancelled', 'activity_reminder'
    ));
CREATE INDEX IF NOT EXISTS inbox_items_target_activity
    ON inbox_items (target_activity_id) WHERE target_activity_id IS NOT NULL;
COMMIT;
