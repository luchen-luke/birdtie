-- Exclusively owned synthetic constraints; no fixed development seed is used.
-- INFERRED records below are raw schema-shape fixtures, not MemoryCandidate
-- verification, authorized inference, acceptance or a callable service.
CREATE FUNCTION pg_temp.memory_assert_rejected(statement text,label text,allowed_states text[])
RETURNS void LANGUAGE plpgsql AS $$
DECLARE failed boolean:=false;
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE<>ALL(allowed_states) THEN RAISE; END IF;
        failed:=true;
    END;
    IF NOT failed THEN RAISE EXCEPTION 'Memory constraint accepted: %',label; END IF;
END $$;

CREATE FUNCTION pg_temp.memory_insert_statement(agent uuid,owner uuid,patch jsonb DEFAULT '{}'::jsonb)
RETURNS text LANGUAGE plpgsql AS $$
DECLARE row jsonb;
BEGIN
    row:=jsonb_build_object('id',gen_random_uuid(),'schema_version','agent-memory-v1',
        'agent_id',agent,'owner_type','PERSON','owner_id',owner,'version',1,
        'memory_type','PREFERENCE','memory_key','synthetic.'||gen_random_uuid()::text,
        'summary','本人合成声明；不是客观身份或校准概率','structured_value','{"declared":true}'::jsonb,
        'confidence',1,'source_type','EXPLICIT','visibility','PRIVATE','status','ACTIVE',
        'valid_from',clock_timestamp(),'valid_until',clock_timestamp()+interval '1 day',
        'last_reinforced_at',NULL,'created_at',clock_timestamp(),'updated_at',clock_timestamp());
    RETURN format('INSERT INTO agent_memories SELECT * FROM jsonb_populate_record(NULL::agent_memories,%L::jsonb)',row||patch);
END $$;

DO $$
DECLARE
    owner_a uuid:=gen_random_uuid(); owner_b uuid:=gen_random_uuid();
    org_owner uuid:=gen_random_uuid(); business_owner uuid:=gen_random_uuid();
    agent_a uuid:=gen_random_uuid(); agent_b uuid:=gen_random_uuid();
    org_agent uuid:=gen_random_uuid(); business_agent uuid:=gen_random_uuid();
    metadata_owner uuid:=gen_random_uuid(); account_owner uuid:=gen_random_uuid();
    metadata_agent uuid:=gen_random_uuid(); account_agent uuid:=gen_random_uuid();
    memory_id uuid:=gen_random_uuid(); inferred_id uuid:=gen_random_uuid();
    expired_id uuid:=gen_random_uuid(); new_id uuid:=gen_random_uuid();
    patch jsonb; item record; invalid_value jsonb; depth_value jsonb;
    before_row jsonb; source_metadata jsonb; source_metadata_after jsonb;
    count_rejected integer:=0; count_positive integer:=0; affected integer;
BEGIN
    INSERT INTO accounts(id,account_type) VALUES(owner_a,'person'),(owner_b,'person'),
        (org_owner,'organization'),(business_owner,'business'),(metadata_owner,'person'),(account_owner,'person');
    INSERT INTO agents(id,agent_type,principal_account_id,status) VALUES
        (agent_a,'personal',owner_a,'active'),(agent_b,'personal',owner_b,'active'),
        (org_agent,'organization',org_owner,'active'),(business_agent,'business',business_owner,'suspended'),
        (metadata_agent,'personal',metadata_owner,'active'),(account_agent,'personal',account_owner,'active');
    SELECT jsonb_agg(to_jsonb(x) ORDER BY agent_id) INTO source_metadata FROM agent_profiles x
        WHERE agent_id IN (agent_a,agent_b,org_agent,business_agent,metadata_agent,account_agent);
    IF jsonb_array_length(source_metadata)<>6 THEN RAISE EXCEPTION 'owned AgentProfile bootstrap incomplete'; END IF;

    FOR patch IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
        jsonb_build_object('version',0),jsonb_build_object('version',2),jsonb_build_object('version',-1),
        jsonb_build_object('schema_version','unsupported'),jsonb_build_object('id',NULL),
        jsonb_build_object('id','00000000-0000-0000-0000-000000000000'),
        jsonb_build_object('owner_id',NULL),jsonb_build_object('agent_id',NULL),
        jsonb_build_object('owner_type','ORGANIZATION'),jsonb_build_object('owner_type','BUSINESS'),
        jsonb_build_object('owner_type','COMMUNITY'),jsonb_build_object('owner_type','person'),
        jsonb_build_object('memory_type','TRAIT'),jsonb_build_object('memory_type','preference'),
        jsonb_build_object('memory_key',''),jsonb_build_object('memory_key','Uppercase'),
        jsonb_build_object('memory_key','兴趣'),jsonb_build_object('memory_key','space key'),
        jsonb_build_object('memory_key',repeat('a',101)),jsonb_build_object('memory_key',E'key\n'),
        jsonb_build_object('summary',''),jsonb_build_object('summary',' '),
        jsonb_build_object('summary',' padded'),jsonb_build_object('summary','padded '),
        jsonb_build_object('summary',repeat('中',401)),jsonb_build_object('summary',E'bad\rline'),
        jsonb_build_object('summary','bad'||chr(127)),jsonb_build_object('summary','bad'||chr(65533)),
        jsonb_build_object('summary',NULL),jsonb_build_object('structured_value',NULL),
        jsonb_build_object('structured_value','[]'::jsonb),jsonb_build_object('structured_value','1'::jsonb),
        jsonb_build_object('source_type','QUERY'),jsonb_build_object('source_type','explicit'),
        jsonb_build_object('visibility','PUBLIC'),jsonb_build_object('visibility','CONNECTIONS'),
        jsonb_build_object('visibility','WORKSPACE_PRIVATE'),jsonb_build_object('visibility',NULL),
        jsonb_build_object('status','CANDIDATE'),jsonb_build_object('status','REJECTED'),
        jsonb_build_object('status','PENDING_REVIEW'),jsonb_build_object('status',NULL),
        jsonb_build_object('confidence',0.9),jsonb_build_object('confidence',-0.1),
        jsonb_build_object('confidence',1.01),jsonb_build_object('confidence',NULL),
        jsonb_build_object('last_reinforced_at',clock_timestamp()),
        jsonb_build_object('valid_from','infinity'),jsonb_build_object('valid_until','infinity'),
        jsonb_build_object('valid_from','-infinity'),jsonb_build_object('valid_until','-infinity'),
        jsonb_build_object('created_at','infinity'),jsonb_build_object('updated_at','infinity'),
        jsonb_build_object('valid_from','10000-01-01T00:00:00Z'),
        jsonb_build_object('valid_until','10000-01-01T00:00:00Z'),
        jsonb_build_object('created_at','0001-01-01 BC'),
        jsonb_build_object('updated_at','10000-01-01T00:00:00Z'),
        jsonb_build_object('valid_until',clock_timestamp()-interval '1 day'),
        jsonb_build_object('valid_until',clock_timestamp()+interval '366 days'),
        jsonb_build_object('updated_at',clock_timestamp()-interval '1 hour'),
        jsonb_build_object('valid_from',NULL),jsonb_build_object('valid_until',NULL),
        jsonb_build_object('created_at',NULL),jsonb_build_object('updated_at',NULL),
        jsonb_build_object('status','DELETED'),
        jsonb_build_object('status','DELETED','summary',''),
        jsonb_build_object('source_type','INFERRED','status','ACTIVE'),
        jsonb_build_object('source_type','INFERRED','status','PENDING_REVIEW','confidence',-0.01),
        jsonb_build_object('source_type','INFERRED','status','PENDING_REVIEW','confidence',1.01),
        jsonb_build_object('source_type','INFERRED','status','PENDING_REVIEW','last_reinforced_at','infinity'),
        jsonb_build_object('source_type','INFERRED','status','PENDING_REVIEW',
            'last_reinforced_at',clock_timestamp()-interval '1 day')
    )) LOOP
        PERFORM pg_temp.memory_assert_rejected(pg_temp.memory_insert_statement(agent_a,owner_a,patch),
            'initial version/binding/type/shape/summary/expiry/lifecycle',ARRAY['23514','23502','P0001']);
        count_rejected:=count_rejected+1;
    END LOOP;
    FOR item IN SELECT * FROM (VALUES
        (agent_a,owner_b,'cross Person owner'),(gen_random_uuid(),owner_a,'unknown Agent'),
        (org_agent,org_owner,'Organization binding presented as Person'),
        (business_agent,business_owner,'Business binding presented as Person')) fixtures(agent,owner,label)
    LOOP
        PERFORM pg_temp.memory_assert_rejected(pg_temp.memory_insert_statement(item.agent,item.owner),item.label,ARRAY['23503']);
        count_rejected:=count_rejected+1;
    END LOOP;

    depth_value:='1'::jsonb;
    FOR affected IN 1..6 LOOP depth_value:=jsonb_build_object('x',depth_value); END LOOP;
    FOR invalid_value IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
        'null'::jsonb,'[]'::jsonb,'1'::jsonb,
        jsonb_build_object('',1),jsonb_build_object(repeat('k',101),1),jsonb_build_object(E'bad\nkey',1),
        jsonb_build_object('x','bad'||chr(127)),jsonb_build_object('x',chr(65533)),
        jsonb_build_object('x',repeat('a',8185)),jsonb_build_object('x',repeat('<',1365)),
        depth_value,
        (SELECT jsonb_object_agg('k'||n,1) FROM generate_series(1,33) n),
        jsonb_build_object('x',(SELECT jsonb_agg(1) FROM generate_series(1,33))),
        (SELECT jsonb_object_agg('k'||n,(SELECT jsonb_agg(1) FROM generate_series(1,32))) FROM generate_series(1,8) n),
        jsonb_build_object('x',repeat('a',16384)),jsonb_build_object('x','1e999'::jsonb)
    )) LOOP
        IF birdtie_agent_memory_value_valid(invalid_value) IS DISTINCT FROM false THEN
            RAISE EXCEPTION 'invalid JSON helper shape accepted';
        END IF;
        PERFORM pg_temp.memory_assert_rejected(pg_temp.memory_insert_statement(agent_a,owner_a,
            jsonb_build_object('structured_value',invalid_value)),'bounded structured object',ARRAY['23514','23502']);
        count_rejected:=count_rejected+1;
    END LOOP;
    FOR invalid_value IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
        '{}'::jsonb,'{"a":[1,true,null,"中文"],"b":{"empty":""}}'::jsonb,
        jsonb_build_object('x',repeat('a',8184)),jsonb_build_object('x',repeat('<',1364)),
        (SELECT jsonb_object_agg('k'||n,1) FROM generate_series(1,32) n),
        jsonb_build_object('x',(SELECT jsonb_agg(1) FROM generate_series(1,32)))
    )) LOOP
        IF birdtie_agent_memory_value_valid(invalid_value) IS DISTINCT FROM true THEN
            RAISE EXCEPTION 'bounded JSON helper positive rejected';
        END IF;
        EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('structured_value',invalid_value));
        count_positive:=count_positive+1;
    END LOOP;
    FOREACH invalid_value IN ARRAY ARRAY[jsonb_build_object('x',repeat('&',1364)),
        jsonb_build_object('x',repeat(chr(8232),1364)),jsonb_build_object('x',repeat(chr(8233),1364))] LOOP
        IF octet_length(birdtie_agent_memory_compact_json(invalid_value))<>8192 OR
            birdtie_agent_memory_value_valid(invalid_value) IS DISTINCT FROM true THEN
            RAISE EXCEPTION 'Go-compatible HTML/Unicode compact byte guard differs';
        END IF;
        count_positive:=count_positive+1;
    END LOOP;
    FOR item IN SELECT memory_type FROM unnest(ARRAY['IDENTITY','PREFERENCE','PLACE','CITY','ACTIVITY',
        'COMMUNITY','ORGANIZATION','RELATIONSHIP_CONTEXT','HISTORY','INTENT','ROUTINE','AVAILABILITY','EXPERIENCE']) memory_type LOOP
        EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('memory_type',item.memory_type));
        count_positive:=count_positive+1;
    END LOOP;
    EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,
        jsonb_build_object('summary',repeat('中',400),'memory_key',repeat('a',100),'visibility','AGENT_ONLY'));
    count_positive:=count_positive+1;
    EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('id',memory_id,'memory_key','manual.cas'));
    SELECT to_jsonb(x) INTO before_row FROM agent_memories x WHERE id=memory_id;
    FOR patch IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
        jsonb_build_object('id',gen_random_uuid()),jsonb_build_object('agent_id',agent_b),
        jsonb_build_object('owner_id',owner_b),jsonb_build_object('owner_type','ORGANIZATION'),
        jsonb_build_object('source_type','INFERRED'),jsonb_build_object('schema_version','unsupported'),
        jsonb_build_object('created_at',clock_timestamp()+interval '1 second')
    )) LOOP
        PERFORM pg_temp.memory_assert_rejected(format(
            'UPDATE agent_memories SET (id,agent_id,owner_id,owner_type,source_type,schema_version,created_at,version)=(SELECT id,agent_id,owner_id,owner_type,source_type,schema_version,created_at,version FROM jsonb_populate_record(NULL::agent_memories,%L::jsonb)) WHERE id=%L',
            before_row||patch||jsonb_build_object('version',2),memory_id),'immutable bindings/source nature',ARRAY['P0001']);
        count_rejected:=count_rejected+1;
    END LOOP;
    PERFORM pg_temp.memory_assert_rejected(format('UPDATE agent_memories SET summary=''改稿'' WHERE id=%L',memory_id),
        'missing CAS increment',ARRAY['P0001']); count_rejected:=count_rejected+1;
    PERFORM pg_temp.memory_assert_rejected(format('UPDATE agent_memories SET version=3 WHERE id=%L',memory_id),
        'jumped independent version',ARRAY['P0001']); count_rejected:=count_rejected+1;
    PERFORM pg_temp.memory_assert_rejected(format('DELETE FROM agent_memories WHERE id=%L',memory_id),
        'ordinary physical delete',ARRAY['P0001']); count_rejected:=count_rejected+1;
    IF (SELECT to_jsonb(x) FROM agent_memories x WHERE id=memory_id)<>before_row THEN
        RAISE EXCEPTION 'failed raw modifications changed source row';
    END IF;
    UPDATE agent_memories SET version=version+1,summary='真实独立版本的本人直接更新' WHERE id=memory_id AND version=1;
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>1 THEN RAISE EXCEPTION 'valid explicit Memory CAS failed'; END IF;
    UPDATE agent_memories SET version=version+1,summary='不应落库的旧版本' WHERE id=memory_id AND version=1;
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>0 THEN RAISE EXCEPTION 'stale raw CAS wrote'; END IF;
    count_positive:=count_positive+2;
    PERFORM pg_temp.memory_assert_rejected(format(
        'UPDATE agent_memories SET version=3,status=''DELETED'' WHERE id=%L',memory_id),
        'tombstone must scrub payload',ARRAY['23514']); count_rejected:=count_rejected+1;
    UPDATE agent_memories SET version=3,status='DELETED',summary='',structured_value='{}' WHERE id=memory_id AND version=2;
    IF NOT EXISTS(SELECT 1 FROM agent_memories WHERE id=memory_id AND version=3 AND status='DELETED'
        AND summary='' AND structured_value='{}') THEN RAISE EXCEPTION 'valid tombstone clear failed'; END IF;
    count_positive:=count_positive+1;
    PERFORM pg_temp.memory_assert_rejected(format(
        'UPDATE agent_memories SET version=4,status=''ACTIVE'',summary=''旧输入复活'' WHERE id=%L',memory_id),
        'deleted ID cannot restore',ARRAY['P0001']); count_rejected:=count_rejected+1;
    PERFORM pg_temp.memory_assert_rejected(format('DELETE FROM agent_memories WHERE id=%L',memory_id),
        'tombstone cannot physically disappear under live parent',ARRAY['P0001']); count_rejected:=count_rejected+1;
    EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('memory_key','manual.cas','id',new_id));
    count_positive:=count_positive+1;
    PERFORM pg_temp.memory_assert_rejected(pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('memory_key','manual.cas')),
        'current explicit semantic key duplicate',ARRAY['23505']); count_rejected:=count_rejected+1;
    EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('memory_key','expired.key','id',expired_id,'status','EXPIRED'));
    EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('memory_key','expired.key'));
    count_positive:=count_positive+2;

    -- Raw future schema-shape control only: never a successful candidate/analysis
    -- entry. No confirmation, grant or current inference resolver exists here.
    EXECUTE pg_temp.memory_insert_statement(agent_a,owner_a,jsonb_build_object('id',inferred_id,
        'memory_key','manual.cas','source_type','INFERRED','status','PENDING_REVIEW','confidence',0.5));
    PERFORM pg_temp.memory_assert_rejected(format(
        'UPDATE agent_memories SET version=2,status=''ACTIVE'' WHERE id=%L',inferred_id),
        'inferred cannot activate',ARRAY['23514']); count_rejected:=count_rejected+1;
    PERFORM pg_temp.memory_assert_rejected(format(
        'UPDATE agent_memories SET version=2,source_type=''EXPLICIT'',status=''ACTIVE'',confidence=1 WHERE id=%L',inferred_id),
        'inference cannot relabel itself as declaration',ARRAY['P0001']); count_rejected:=count_rejected+1;
    count_positive:=count_positive+1;

    SELECT jsonb_agg(to_jsonb(x) ORDER BY agent_id) INTO source_metadata_after FROM agent_profiles x
        WHERE agent_id IN (agent_a,agent_b,org_agent,business_agent,metadata_agent,account_agent);
    IF source_metadata IS DISTINCT FROM source_metadata_after THEN
        RAISE EXCEPTION 'Memory writes changed native AgentProfile source/version';
    END IF;
    count_positive:=count_positive+1;
    EXECUTE pg_temp.memory_insert_statement(metadata_agent,metadata_owner);
    EXECUTE pg_temp.memory_insert_statement(account_agent,account_owner);
    DELETE FROM agent_profiles WHERE agent_id=metadata_agent;
    DELETE FROM accounts WHERE id=account_owner;
    IF EXISTS(SELECT 1 FROM agent_memories WHERE agent_id IN(metadata_agent,account_agent)) OR
        EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=account_agent) THEN
        RAISE EXCEPTION 'own actual parent cascade retained Memory or identity';
    END IF;
    count_positive:=count_positive+2;
    DELETE FROM agents WHERE id IN(agent_a,agent_b,org_agent,business_agent,metadata_agent,account_agent);
    DELETE FROM accounts WHERE id IN(owner_a,owner_b,org_owner,business_owner,metadata_owner,account_owner);
    IF EXISTS(SELECT 1 FROM agent_memories WHERE agent_id IN(agent_a,agent_b,org_agent,business_agent,metadata_agent,account_agent)) OR
        EXISTS(SELECT 1 FROM accounts WHERE id IN(owner_a,owner_b,org_owner,business_owner,metadata_owner,account_owner)) OR
        EXISTS(SELECT 1 FROM agents WHERE id IN(agent_a,agent_b,org_agent,business_agent,metadata_agent,account_agent)) THEN
        RAISE EXCEPTION 'owned strict Memory fixture left rows';
    END IF;
    RAISE NOTICE '[PASS] % strict raw Memory rejections; % positive shape/CAS/scrub/cascade assertions; inferred fixture is schema-only, no inference acceptance',count_rejected,count_positive;
END $$;
