BEGIN;
DROP TABLE persistent_session_access;
DROP TABLE persistent_session_tokens;
DROP TABLE persistent_session_families;
DROP FUNCTION birdtie_persistent_session_family_guard();
COMMIT;
