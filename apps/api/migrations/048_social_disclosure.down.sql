BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM person_social_disclosure) THEN
        RAISE EXCEPTION 'cannot remove social disclosure preferences with data';
    END IF;
END $$;
DROP TABLE person_social_disclosure;
DROP FUNCTION birdtie_social_disclosure_person();
COMMIT;
