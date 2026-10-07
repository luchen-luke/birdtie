BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM venues) OR EXISTS (SELECT 1 FROM venue_candidates) THEN
        RAISE EXCEPTION 'cannot remove sourced Venue capabilities while candidate or published data exists';
    END IF;
END $$;
DROP TABLE venues;
DROP TABLE venue_candidates;
COMMIT;
