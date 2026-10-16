BEGIN;
DROP FUNCTION IF EXISTS birdtie_legacy_moment_author_contract();
DROP TABLE IF EXISTS legacy_moment_author_facts;
DROP TABLE IF EXISTS legacy_moment_author_snapshots;
DROP TABLE IF EXISTS legacy_moment_author_runs;
DROP FUNCTION IF EXISTS birdtie_legacy_moment_author_guard();
COMMIT;
