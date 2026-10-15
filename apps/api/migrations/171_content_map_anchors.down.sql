BEGIN;
-- Explicit rollback of only this read projection. Preserved archives and
-- publication, account, and media ownership are unaffected.
DROP FUNCTION IF EXISTS birdtie_content_map_contract();
DROP INDEX IF EXISTS location_reference_content_viewport;
DROP INDEX IF EXISTS moment_media_context_place_content;
DROP INDEX IF EXISTS moment_location_place_content;
DROP INDEX IF EXISTS native_place_content_viewport;
DROP INDEX IF EXISTS legacy_map_place_viewport;
DROP TABLE IF EXISTS legacy_map_anchor_facts;
DROP TABLE IF EXISTS legacy_map_anchor_runs;
DROP FUNCTION IF EXISTS birdtie_legacy_map_anchor_guard();
DROP FUNCTION IF EXISTS birdtie_legacy_map_moment_binding(moments);
COMMIT;
