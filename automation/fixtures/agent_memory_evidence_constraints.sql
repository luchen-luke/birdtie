-- Storage shape assertions only. Arbitrary source UUIDs below do not establish
-- a source, consent, inferred interest, attendance or model permission.
BEGIN;
SELECT set_config('birdtie.evidence_test_owner', :'nativeOwner', true);
SELECT set_config('birdtie.evidence_test_agent', :'nativeAgent', true);
SELECT set_config('birdtie.evidence_test_memory', :'nativeMemory', true);
CREATE FUNCTION pg_temp.insert_evidence_shape(overrides jsonb) RETURNS void LANGUAGE plpgsql AS $$
DECLARE value jsonb; row agent_memory_evidence;
BEGIN
    value:=jsonb_build_object('id',gen_random_uuid(),'schema_version','agent-memory-evidence-v1',
      'memory_id',current_setting('birdtie.evidence_test_memory'),'memory_version',2,
      'agent_id',current_setting('birdtie.evidence_test_agent'),'owner_type','PERSON',
      'owner_id',current_setting('birdtie.evidence_test_owner'),'version',1,'status','CURRENT',
      'source_type','MOMENT','source_id',gen_random_uuid(),'source_version_kind','REVISION',
      'source_revision',1,'signal_type','MANUAL_REFERENCE','weight',1,
      'observed_at',now(),'event_time',now()-interval '1 second','created_at',now(),'updated_at',now())||overrides;
    row:=jsonb_populate_record(NULL::agent_memory_evidence,value);
    INSERT INTO agent_memory_evidence SELECT (row).*;
END $$;
CREATE FUNCTION pg_temp.expect_evidence_denial(overrides jsonb) RETURNS void LANGUAGE plpgsql AS $$
DECLARE failed boolean:=false;
BEGIN
    BEGIN PERFORM pg_temp.insert_evidence_shape(overrides);
    EXCEPTION WHEN OTHERS THEN failed:=true;
    END;
    IF NOT failed THEN RAISE EXCEPTION 'Evidence constraint unexpectedly accepted %',overrides; END IF;
END $$;
DO $$
DECLARE overrides jsonb; negatives integer:=0; positives integer:=0; evidence_id uuid; before_version bigint;
BEGIN
    FOR overrides IN SELECT value FROM jsonb_array_elements('[
      {"id":null},{"id":"00000000-0000-0000-0000-000000000000"},{"id":"bad"},
      {"schema_version":"unknown"},{"schema_version":null},
      {"memory_id":null},{"memory_id":"00000000-0000-0000-0000-000000000000"},
      {"memory_version":0},{"memory_version":-1},{"memory_version":1},{"memory_version":3},
      {"agent_id":null},{"agent_id":"00000000-0000-0000-0000-000000000000"},
      {"owner_type":"ORGANIZATION"},{"owner_type":"BUSINESS"},{"owner_type":null},
      {"owner_id":null},{"owner_id":"00000000-0000-0000-0000-000000000000"},
      {"version":0},{"version":2},{"version":null},{"status":"ACTIVE"},{"status":"REMOVED"},{"status":null},
      {"source_type":"UNKNOWN"},{"source_type":null},{"source_type":""},
      {"source_id":null},{"source_id":"00000000-0000-0000-0000-000000000000"},
      {"source_version_kind":"UNKNOWN"},{"source_version_kind":null},{"source_revision":0},{"source_revision":null},
      {"source_token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
      {"source_type":"ACTIVITY_PARTICIPATION","source_version_kind":"UPDATED_AT_DIGEST","source_revision":null,"source_token":null},
      {"source_type":"SAVED_PLACE","source_version_kind":"CREATED_AT_DIGEST","source_revision":null,"source_token":"bad"},
      {"signal_type":null},{"signal_type":"INFERRED_CONFIRMED"},{"signal_type":"JOIN_PROVES_ATTENDANCE"},
      {"weight":0},{"weight":0.5},{"weight":-1},{"weight":2},{"weight":"NaN"},{"weight":null},
      {"event_time":null},{"event_time":"infinity"},{"event_time":"-infinity"},
      {"observed_at":null},{"observed_at":"infinity"},{"created_at":null},{"updated_at":"infinity"}
    ]'::jsonb) LOOP
        PERFORM pg_temp.expect_evidence_denial(overrides); negatives:=negatives+1;
    END LOOP;
    PERFORM pg_temp.expect_evidence_denial(jsonb_build_object('event_time',now()+interval '1 second'));negatives:=negatives+1;
    PERFORM pg_temp.expect_evidence_denial(jsonb_build_object('created_at',now()-interval '1 second'));negatives:=negatives+1;
    PERFORM pg_temp.expect_evidence_denial(jsonb_build_object('updated_at',now()-interval '1 second'));negatives:=negatives+1;

    PERFORM pg_temp.insert_evidence_shape('{}');positives:=positives+1;
    PERFORM pg_temp.insert_evidence_shape(jsonb_build_object('source_type','ACTIVITY_PARTICIPATION','source_version_kind','UPDATED_AT_DIGEST','source_revision',NULL,'source_token',repeat('a',64)));positives:=positives+1;
    PERFORM pg_temp.insert_evidence_shape(jsonb_build_object('source_type','SAVED_PLACE','source_version_kind','CREATED_AT_DIGEST','source_revision',NULL,'source_token',repeat('b',64)));positives:=positives+1;

    SELECT id,version INTO evidence_id,before_version FROM agent_memory_evidence WHERE source_type='MOMENT' LIMIT 1;
    BEGIN UPDATE agent_memory_evidence SET version=version+1,source_revision=2 WHERE id=evidence_id;
      RAISE EXCEPTION 'direct Evidence replacement unexpectedly accepted';
    EXCEPTION WHEN raise_exception THEN
      IF SQLERRM='direct Evidence replacement unexpectedly accepted' THEN RAISE; END IF;
    END;negatives:=negatives+1;
    BEGIN DELETE FROM agent_memory_evidence WHERE id=evidence_id;
      RAISE EXCEPTION 'physical Evidence deletion unexpectedly accepted';
    EXCEPTION WHEN raise_exception THEN
      IF SQLERRM='physical Evidence deletion unexpectedly accepted' THEN RAISE; END IF;
    END;negatives:=negatives+1;

    UPDATE agent_memory_evidence SET version=version+1,status='REMOVED',source_type=NULL,source_id=NULL,source_version_kind=NULL,
      source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL WHERE id=evidence_id;
    IF NOT EXISTS(SELECT 1 FROM agent_memory_evidence WHERE id=evidence_id AND version=before_version+1 AND status='REMOVED' AND source_id IS NULL) THEN
      RAISE EXCEPTION 'removal did not preserve a scrubbed control'; END IF;positives:=positives+1;
    BEGIN UPDATE agent_memory_evidence SET version=version+1 WHERE id=evidence_id;
      RAISE EXCEPTION 'removed Evidence unexpectedly modified';
    EXCEPTION WHEN raise_exception THEN
      IF SQLERRM='removed Evidence unexpectedly modified' THEN RAISE; END IF;
    END;negatives:=negatives+1;
    UPDATE agent_memories SET version=version+1,summary='本人修改-仅SQL约束fixture' WHERE id=current_setting('birdtie.evidence_test_memory')::uuid;
    IF EXISTS(SELECT 1 FROM agent_memory_evidence WHERE status<>'REMOVED' OR source_id IS NOT NULL OR source_token IS NOT NULL OR event_time IS NOT NULL OR weight<>0) THEN
      RAISE EXCEPTION 'Memory version update did not retire all metadata'; END IF;positives:=positives+1;
    RAISE NOTICE 'EVIDENCE_SQL_DENIALS=% POSITIVE_ASSERTIONS=%; shapes only, not trusted native source acceptance',negatives,positives;
END $$;
ROLLBACK;
