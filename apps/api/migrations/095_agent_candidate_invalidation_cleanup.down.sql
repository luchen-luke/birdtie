-- Restore the exact 094-era maintenance function, not expired candidate payload.
BEGIN;
LOCK TABLE agent_memory_candidates,agent_effect_ledger IN ACCESS SHARE MODE;
CREATE OR REPLACE FUNCTION birdtie_expire_candidate_pipeline(batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE total integer; more integer;
BEGIN
 IF batch_size<1 OR batch_size>100 THEN RAISE EXCEPTION 'bounded candidate cleanup required'; END IF;
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id
 JOIN consent_grants g ON g.id=e.retention_grant_id JOIN agent_candidate_retention_bindings rb ON rb.grant_id=g.id JOIN agent_candidate_retention_previews rp ON rp.id=rb.preview_id JOIN consent_grants ag ON ag.id=rp.analysis_grant_id
 WHERE c.status='CANDIDATE' AND(c.valid_until<=clock_timestamp() OR g.revoked_at IS NOT NULL OR g.expires_at<=clock_timestamp() OR ag.revoked_at IS NOT NULL OR ag.expires_at<=clock_timestamp())
 ORDER BY c.id LIMIT batch_size FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS total=ROW_COUNT;
 IF total<batch_size THEN
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id WHERE e.handler_version='mom-candidate-multi-v1' AND c.status='CANDIDATE' AND(c.valid_until<=clock_timestamp() OR NOT birdtie_candidate_pipeline_current(e.retention_grant_id)) ORDER BY c.id LIMIT batch_size-total FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS more=ROW_COUNT;total:=total+more; END IF; RETURN total;
END $$;
COMMIT;
