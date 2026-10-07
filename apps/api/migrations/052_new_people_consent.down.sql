BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM person_new_people_consent) THEN
  RAISE EXCEPTION 'cannot remove new people consent while choices exist';
 END IF;
END $$;
DROP TABLE person_new_people_consent;
COMMIT;
