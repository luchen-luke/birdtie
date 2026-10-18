BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM journey_publication_snapshots) THEN RAISE EXCEPTION 'Published Journey facts require an explicit preservation plan; refusing destructive rollback' USING ERRCODE='23514';END IF;END $$;
DROP FUNCTION birdtie_journey_lifecycle(uuid,timestamptz);
DROP TABLE journey_publication_current;
DROP FUNCTION birdtie_journey_publication_current_guard();
DROP TRIGGER journey_publication_snapshot_guard ON journey_publication_snapshots;
DROP FUNCTION birdtie_journey_publication_snapshot_guard();
DROP TABLE journey_publication_snapshots;
COMMIT;
