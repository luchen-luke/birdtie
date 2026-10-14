DO $$
BEGIN
 IF to_regclass('legacy_import_runs') IS NOT NULL AND EXISTS(SELECT 1 FROM legacy_import_runs) THEN RAISE EXCEPTION 'cannot remove legacy import history';END IF;
 IF to_regclass('legacy_entity_mappings') IS NOT NULL AND EXISTS(SELECT 1 FROM legacy_entity_mappings) THEN RAISE EXCEPTION 'cannot remove legacy permanent mappings';END IF;
 IF to_regclass('legacy_note_archives') IS NOT NULL AND EXISTS(SELECT 1 FROM legacy_note_archives) THEN RAISE EXCEPTION 'cannot remove owner legacy archives';END IF;
END;
$$;
DROP TABLE IF EXISTS legacy_note_archives;
DROP TABLE IF EXISTS legacy_entity_mappings;
DROP TABLE IF EXISTS legacy_import_runs;
DROP FUNCTION IF EXISTS birdtie_legacy_run_complete();
DROP FUNCTION IF EXISTS birdtie_legacy_import_binding();
DROP FUNCTION IF EXISTS birdtie_legacy_immutable();
DROP FUNCTION IF EXISTS birdtie_legacy_nonnegative_counts(jsonb);
DROP FUNCTION IF EXISTS birdtie_legacy_relationships_safe(jsonb);
DROP FUNCTION IF EXISTS birdtie_legacy_fixed_keys(jsonb,text[]);
