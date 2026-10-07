BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM inbox_items WHERE target_activity_id IS NOT NULL
        OR resource_type IN ('activity_change', 'activity_cancelled', 'activity_reminder')) THEN
        RAISE EXCEPTION '023 rollback refused: activity notifications exist';
    END IF;
END $$;
DROP INDEX inbox_items_target_activity;
ALTER TABLE inbox_items DROP COLUMN target_activity_id;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check
    CHECK (resource_type IN (
        'place_candidate', 'activity_candidate', 'community',
        'connection_request', 'conversation_message'
    ));
COMMIT;
