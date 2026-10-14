BEGIN;
LOCK TABLE human_participation_operation_receipts IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM human_participation_operation_receipts) THEN
 RAISE EXCEPTION 'used human RSVP operation fences must be retained' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE human_participation_operation_receipts;
DROP FUNCTION birdtie_human_participation_operation_guard();
COMMIT;
