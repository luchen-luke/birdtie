BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM activity_organizers WHERE community_id IS NOT NULL) THEN
        RAISE EXCEPTION '031 rollback would discard Community Activity organizer';
    END IF;
END $$;
DROP TRIGGER activity_organizer_row_required ON activity_organizers;
DROP TRIGGER activity_organizer_required ON activities;
DROP TRIGGER activity_initial_organizer ON activities;
DROP TRIGGER activity_organizer_validate ON activity_organizers;
DROP FUNCTION birdtie_activity_requires_organizer();
DROP FUNCTION birdtie_initial_activity_organizer();
DROP FUNCTION birdtie_validate_activity_organizer();
DROP TABLE activity_organizers;
COMMIT;
