BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM person_ties) OR
       EXISTS (SELECT 1 FROM connection_requests WHERE scope='friend') THEN
        RAISE EXCEPTION 'cannot remove Tie Graph while friend data exists';
    END IF;
END $$;
DROP TRIGGER person_tie_validate ON person_ties;
DROP FUNCTION birdtie_validate_person_tie();
DROP TABLE person_ties;
ALTER TABLE connection_requests DROP CONSTRAINT connection_request_scope_city_shape;
ALTER TABLE connection_requests ALTER COLUMN city_id SET NOT NULL;
ALTER TABLE connection_requests DROP CONSTRAINT connection_requests_scope_check;
ALTER TABLE connection_requests ADD CONSTRAINT connection_requests_scope_check
    CHECK (scope='conversation');
COMMIT;
