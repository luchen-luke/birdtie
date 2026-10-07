BEGIN;
LOCK TABLE agent_enrichment_observations IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM agent_enrichment_observations) THEN RAISE EXCEPTION 'retained observation metadata requires explicit bounded prune; original audits untouched' USING ERRCODE='P0001';END IF;END $$;
DROP TABLE agent_enrichment_observations;
DROP FUNCTION birdtie_enrichment_observation_guard();
COMMIT;
