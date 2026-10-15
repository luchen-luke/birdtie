BEGIN;
DROP FUNCTION birdtie_legacy_map_visibility_contract();
DROP TABLE legacy_map_place_facts;
DROP TABLE legacy_map_visibility_runs;
DROP FUNCTION birdtie_legacy_map_binding_guard();
DROP FUNCTION birdtie_legacy_map_place_binding(places);
COMMIT;
