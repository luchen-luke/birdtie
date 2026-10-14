BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM legacy_personal_archive_runs) OR EXISTS(SELECT 1 FROM legacy_personal_archives)
 THEN RAISE EXCEPTION 'legacy personal archive history rollback refused'; END IF; END $$;
DROP TABLE legacy_personal_archives;
DROP TABLE legacy_personal_archive_runs;
DROP FUNCTION birdtie_legacy_personal_archive_binding();
DROP FUNCTION birdtie_legacy_personal_archive_complete();
COMMIT;
