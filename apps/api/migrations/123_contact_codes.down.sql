BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM person_contact_codes) OR EXISTS(SELECT 1 FROM person_contact_code_operations) THEN
  RAISE EXCEPTION 'Refusing to discard contact code state or operation history';
 END IF;
END $$;
DROP TABLE IF EXISTS person_contact_code_operations;
DROP TABLE IF EXISTS person_contact_codes;
COMMIT;
