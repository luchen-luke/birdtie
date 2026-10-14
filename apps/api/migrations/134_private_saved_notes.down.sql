BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM private_saved_notes WHERE note<>'') OR EXISTS(SELECT 1 FROM private_saved_note_operations) THEN
  RAISE EXCEPTION 'private saved note contents and operation receipts must not be discarded by downgrade' USING ERRCODE='23514';
 END IF;
END $$;
DROP TABLE private_saved_note_operations;
DROP TABLE private_saved_notes;
COMMIT;
