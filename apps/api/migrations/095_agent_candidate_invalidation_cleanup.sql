-- AIR018: bounded cleanup of original committed pipeline candidate support.
-- No source, authority, effect, Run, Memory or audit ledger is replaced.
BEGIN;
LOCK TABLE agent_memory_candidates,agent_effect_ledger IN ACCESS SHARE MODE;
CREATE OR REPLACE FUNCTION birdtie_expire_candidate_pipeline(batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE total integer; more integer;
BEGIN
 IF batch_size IS NULL OR batch_size<1 OR batch_size>100 THEN RAISE EXCEPTION 'bounded candidate cleanup required'; END IF;
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id
 WHERE e.handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2') AND c.status='CANDIDATE'
 AND(c.valid_until<=clock_timestamp() OR birdtie_candidate_pipeline_current(e.retention_grant_id) IS NOT TRUE)
 ORDER BY c.id LIMIT batch_size FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS total=ROW_COUNT;
 IF total<batch_size THEN
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id WHERE e.handler_version='mom-candidate-multi-v1' AND c.status='CANDIDATE' AND(c.valid_until<=clock_timestamp() OR birdtie_candidate_pipeline_current(e.retention_grant_id) IS NOT TRUE) ORDER BY c.id LIMIT batch_size-total FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS more=ROW_COUNT;total:=total+more; END IF; RETURN total;
END $$;
COMMIT;
