BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM city_activity_candidate_organizers) THEN
        RAISE EXCEPTION 'cannot remove retained explicit candidate organizer selectors';
    END IF;
END $$;
DROP TABLE city_activity_candidate_organizers;
DROP FUNCTION birdtie_guard_city_candidate_organizer();
COMMIT;
