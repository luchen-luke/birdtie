BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM social_intent_creation_receipts) THEN RAISE EXCEPTION 'used private creation receipts prevent down' USING ERRCODE='55000';END IF;END $$;
DROP TABLE social_intent_creation_receipts;
DROP FUNCTION birdtie_social_intent_creation_receipt_guard();
COMMIT;
