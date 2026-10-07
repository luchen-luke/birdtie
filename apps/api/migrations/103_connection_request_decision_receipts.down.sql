BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM connection_request_decision_receipts) THEN
 RAISE EXCEPTION 'used decision receipts must be retained' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE connection_request_decision_receipts;
DROP FUNCTION birdtie_connection_decision_receipt_guard();
COMMIT;
