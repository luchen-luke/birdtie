BEGIN;
DROP TRIGGER IF EXISTS activity_participation_open_guard ON activity_participations;
DROP FUNCTION IF EXISTS birdtie_validate_activity_open_for_participation();
COMMIT;
