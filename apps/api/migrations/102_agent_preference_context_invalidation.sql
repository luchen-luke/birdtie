-- AIR020 exact private preference metadata invalidation; no new authority.
BEGIN;
LOCK TABLE agent_profiles,agent_private_profiles,agent_domain_outbox,agent_consumer_inbox IN ACCESS EXCLUSIVE MODE;
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_schema_version_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_schema_version_check CHECK(schema_version IN('agent-outbox-v1','agent-memory-outbox-v1','agent-preference-outbox-v1'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_event_type_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_event_type_check CHECK(event_type IN('MOMENT_CREATED','MOMENT_UPDATED','MOMENT_WITHDRAWN','MEMORY_UPDATED','MEMORY_CONTEXT_INVALIDATION','PREFERENCE_UPDATED','PREFERENCE_CONTEXT_INVALIDATION'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_type_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_type_check CHECK(source_type IN('MOMENT','MEMORY','PRIVATE_PREFERENCE'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_status_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_status_check CHECK(source_status IN('draft','withdrawn','ACTIVE','DELETED','configured','cleared'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_delivery_state_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_delivery_state_check CHECK(delivery_state IN('PENDING','LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER','CANDIDATE_STAGED','INVALIDATION_COMPLETE'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_handler_version_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_handler_version_check CHECK(handler_version IN('mom-control-v1','mom-control-v2','mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1','memory-invalidation-v1','preference-invalidation-v1'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_control_state_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_control_state_check CHECK(control_state IN('LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER','CANDIDATE_STAGED','PENDING','INVALIDATION_COMPLETE'));
DO $$ DECLARE c text; BEGIN
 SELECT conname INTO STRICT c FROM pg_constraint WHERE conrelid='agent_domain_outbox'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%event_type%' AND pg_get_constraintdef(oid) LIKE '%source_status%';
 EXECUTE format('ALTER TABLE agent_domain_outbox DROP CONSTRAINT %I',c);
 EXECUTE format('ALTER TABLE agent_domain_outbox ADD CONSTRAINT %I CHECK(%s)',c,$expr$(schema_version='agent-outbox-v1' AND source_type='MOMENT' AND ((event_type='MOMENT_CREATED' AND source_status='draft' AND source_revision=1) OR(event_type='MOMENT_UPDATED' AND source_status='draft' AND source_revision>1) OR(event_type='MOMENT_WITHDRAWN' AND source_status='withdrawn' AND source_revision>1))) OR(schema_version='agent-memory-outbox-v1' AND source_type='MEMORY' AND event_type IN('MEMORY_UPDATED','MEMORY_CONTEXT_INVALIDATION') AND source_status IN('ACTIVE','DELETED')) OR(schema_version='agent-preference-outbox-v1' AND source_type='PRIVATE_PREFERENCE' AND source_id=agent_id AND source_revision>=2 AND event_type IN('PREFERENCE_UPDATED','PREFERENCE_CONTEXT_INVALIDATION') AND source_status IN('configured','cleared'))$expr$);
END $$;
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_check1;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_check1 CHECK((handler_version NOT IN('memory-invalidation-v1','preference-invalidation-v1') AND ((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED') OR(control_state='CANDIDATE_STAGED' AND reason_code='CANDIDATE_STAGED' AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1')))) OR(handler_version IN('memory-invalidation-v1','preference-invalidation-v1') AND ((control_state='LEASED' AND reason_code='') OR(control_state='PENDING' AND reason_code='CLEANUP_MORE') OR(control_state='INVALIDATION_COMPLETE' AND reason_code='CLEANUP_COMPLETE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code IN('ROOT_BUDGET_EXHAUSTED','ATTEMPTS_EXHAUSTED')))));


CREATE FUNCTION birdtie_preference_outbox_operation(a uuid,v bigint,k text) RETURNS uuid LANGUAGE sql IMMUTABLE AS $$
 SELECT birdtie_memory_outbox_uuid(convert_to('birdtie.preference.operation.v1','UTF8')||decode('00','hex')||convert_to(a::text,'UTF8')||decode('00','hex')||convert_to(v::text,'UTF8')||decode('00','hex')||convert_to(k,'UTF8'))
$$;
CREATE FUNCTION birdtie_preference_outbox_fingerprint(a uuid,o uuid,v bigint,st text,at timestamptz,epoch text) RETURNS text LANGUAGE sql STABLE SET TimeZone='UTC' AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('agent',a,'owner',o,'version',v,'status',st,'updatedAt',at,'epoch',epoch)::text,'UTF8')),'hex')
$$;
CREATE FUNCTION birdtie_preference_outbox_event(k text,o uuid,a uuid,v bigint,st text,fp text) RETURNS uuid LANGUAGE sql IMMUTABLE AS $$
 SELECT birdtie_memory_outbox_uuid(convert_to('birdtie.outbox.identity.v1','UTF8')||decode('00','hex')||convert_to(
 '{"Schema":"agent-preference-outbox-v1","Type":"'||k||'","Tenant":{"type":"PERSON","id":"'||o::text||'"},"Subject":{"type":"PERSON","id":"'||o::text||'"},"Agent":"'||a::text||'","Operation":"'||birdtie_preference_outbox_operation(a,v,k)::text||'","Source":{"type":"PRIVATE_PREFERENCE","id":"'||a::text||'","owner":{"type":"PERSON","id":"'||o::text||'"},"revision":'||v::text||',"status":"'||st||'","fingerprint":"'||fp||'"}}','UTF8'))
$$;
-- Current source predicate, metadata only. Configured uses written revision and
-- private row epoch, while clear uses advanced metadata and actual row absence.
CREATE FUNCTION birdtie_preference_outbox_current(a uuid,o uuid,v bigint,st text,at timestamptz,fp text) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT (a IS NOT NULL AND EXISTS(SELECT 1 FROM agent_private_profiles pp WHERE pp.agent_id=a AND pp.owner_id=o AND pp.owner_type='PERSON'
 AND st='configured' AND pp.written_profile_version=v AND pp.updated_at=at AND birdtie_preference_outbox_fingerprint(a,o,v,st,at,pp.xmin::text)=fp))
 OR EXISTS(SELECT 1 FROM agent_profiles ap WHERE ap.agent_id=a AND ap.owner_id=o AND ap.owner_type='PERSON' AND st='cleared'
 AND ap.profile_version=v AND ap.updated_at=at AND NOT EXISTS(SELECT 1 FROM agent_private_profiles pp WHERE pp.agent_id=a)
 AND birdtie_preference_outbox_fingerprint(a,o,v,st,at,ap.xmin::text)=fp)
$$;
CREATE FUNCTION birdtie_capture_preference_outbox() RETURNS trigger LANGUAGE plpgsql SET TimeZone='UTC' AS $$
DECLARE a uuid;o uuid;v bigint;st text;at timestamptz;epoch text;fp text;op uuid;n timestamptz;
BEGIN
 IF TG_OP='DELETE' THEN
   a:=OLD.agent_id;o:=OLD.owner_id;st:='cleared';
   SELECT ap.profile_version,ap.updated_at,ap.xmin::text INTO v,at,epoch FROM agent_profiles ap
   WHERE ap.agent_id=a AND ap.owner_id=o AND ap.owner_type='PERSON' AND ap.profile_version>OLD.written_profile_version;
   IF NOT FOUND OR EXISTS(SELECT 1 FROM agent_private_profiles pp WHERE pp.agent_id=a) THEN RETURN OLD;END IF;
 ELSE
   IF TG_OP='UPDATE' AND NEW.written_profile_version=OLD.written_profile_version THEN RETURN NEW;END IF;
   a:=NEW.agent_id;o:=NEW.owner_id;v:=NEW.written_profile_version;st:='configured';at:=NEW.updated_at;
   SELECT pp.xmin::text INTO STRICT epoch FROM agent_private_profiles pp WHERE pp.agent_id=a AND pp.owner_id=o;
 END IF;
 IF v<2 OR NOT EXISTS(SELECT 1 FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=ac.id AND ap.owner_type='PERSON'
 WHERE ac.id=o AND ac.account_type='person' AND ac.status='active' AND ag.id=a AND ag.agent_type='personal' AND ag.status='active') THEN
   IF TG_OP='DELETE' THEN RETURN OLD;ELSE RETURN NEW;END IF;
 END IF;
 fp:=birdtie_preference_outbox_fingerprint(a,o,v,st,at,epoch);op:=birdtie_preference_outbox_operation(a,v,'PREFERENCE_UPDATED');n:=clock_timestamp();
 -- Fixed five-second refresh coalescing preserves old immutable event metadata.
 -- Clear, claimed, child, inbox and bound history are never coalesced.
 IF st='configured' THEN
   UPDATE agent_domain_outbox d SET delivery_state='INVALIDATED',updated_at=n
   WHERE d.schema_version='agent-preference-outbox-v1' AND d.event_type='PREFERENCE_UPDATED' AND d.causation_id IS NULL
   AND d.subject_id=o AND d.agent_id=a AND d.source_id=a AND d.source_status='configured' AND d.source_revision<v
   AND d.delivery_state='PENDING' AND d.attempt=0 AND d.fence=0 AND d.lease_owner IS NULL AND d.lease_until IS NULL
   AND d.occurred_at<=at AND at-d.occurred_at<=interval '5 seconds'
   AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id);
 END IF;
 INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,occurred_at,received_at,expires_at)
 VALUES('agent-preference-outbox-v1',birdtie_preference_outbox_event('PREFERENCE_UPDATED',o,a,v,st,fp),'PREFERENCE_UPDATED',o,o,o,a,'PRIVATE_PREFERENCE',a,v,fp,st,op,op,at,n,at+interval '15 minutes');
 IF TG_OP='DELETE' THEN RETURN OLD;ELSE RETURN NEW;END IF;
END $$;
CREATE TRIGGER preference_outbox_capture AFTER INSERT OR UPDATE OR DELETE ON agent_private_profiles FOR EACH ROW EXECUTE FUNCTION birdtie_capture_preference_outbox();
CREATE UNIQUE INDEX preference_outbox_logical_mutation ON agent_domain_outbox(subject_id,agent_id,source_id,source_revision,event_type) WHERE schema_version='agent-preference-outbox-v1';
CREATE INDEX preference_outbox_root_budget ON agent_domain_outbox(subject_id,agent_id,root_trace_id) WHERE schema_version='agent-preference-outbox-v1';
CREATE OR REPLACE FUNCTION birdtie_guard_agent_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN

    IF NEW.schema_version='agent-preference-outbox-v1' THEN
      IF NEW.delivery_state NOT IN('PENDING','LEASED','INVALIDATED','EXPIRED','DEAD_LETTER','INVALIDATION_COMPLETE') THEN RAISE EXCEPTION 'unsupported Preference control state' USING ERRCODE='55000';END IF;
      IF TG_OP='INSERT' THEN
        IF NEW.delivery_state<>'PENDING' OR NEW.attempt<>0 OR NEW.fence<>0 OR NEW.lease_owner IS NOT NULL OR NEW.lease_until IS NOT NULL OR NEW.received_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp()
        OR NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE a.id=NEW.subject_id AND a.account_type='person' AND a.status='active' AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0)
        OR NEW.source_id<>NEW.agent_id OR NOT birdtie_preference_outbox_current(NEW.agent_id,NEW.subject_id,NEW.source_revision,NEW.source_status,NEW.occurred_at,NEW.source_fingerprint)
        OR NEW.event_id<>birdtie_preference_outbox_event(NEW.event_type,NEW.subject_id,NEW.agent_id,NEW.source_revision,NEW.source_status,NEW.source_fingerprint)
        OR NEW.logical_operation_id<>birdtie_preference_outbox_operation(NEW.source_id,NEW.source_revision,NEW.event_type)
        OR NEW.root_trace_id<>birdtie_preference_outbox_operation(NEW.source_id,NEW.source_revision,'PREFERENCE_UPDATED') THEN RAISE EXCEPTION 'exact native Preference capture required' USING ERRCODE='55000';END IF;
        IF NEW.event_type='PREFERENCE_UPDATED' THEN
          IF NEW.causation_id IS NOT NULL THEN RAISE EXCEPTION 'Preference mutation root cannot be self-generated' USING ERRCODE='55000';END IF;
        ELSIF NEW.event_type='PREFERENCE_CONTEXT_INVALIDATION' THEN
          IF NOT EXISTS(SELECT 1 FROM agent_domain_outbox p JOIN agent_consumer_inbox i ON i.event_id=p.event_id AND i.subject_id=p.subject_id
            WHERE p.event_id=NEW.causation_id AND p.event_type='PREFERENCE_UPDATED' AND p.schema_version=NEW.schema_version AND p.causation_id IS NULL AND p.root_trace_id=NEW.root_trace_id AND p.subject_id=NEW.subject_id AND p.agent_id=NEW.agent_id
            AND p.source_id=NEW.source_id AND p.source_revision=NEW.source_revision AND p.source_fingerprint=NEW.source_fingerprint AND p.source_status=NEW.source_status AND p.occurred_at=NEW.occurred_at AND p.expires_at=NEW.expires_at
            AND p.delivery_state='INVALIDATION_COMPLETE' AND i.handler_version='preference-invalidation-v1' AND i.control_state='INVALIDATION_COMPLETE' AND i.reason_code='CLEANUP_COMPLETE' AND i.fence=p.fence AND i.attempt=p.attempt AND NEW.received_at=i.updated_at AND i.updated_at<=p.updated_at
            AND p.xmin::text=(txid_current()%4294967296)::text AND i.xmin::text=(txid_current()%4294967296)::text) THEN RAISE EXCEPTION 'only sameTx completed root may create fixed depth1 invalidation child' USING ERRCODE='55000';END IF;
        ELSE RAISE EXCEPTION 'Preference causal depth exceeded' USING ERRCODE='55000';END IF;
        NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;NEW.next_attempt_at:=NEW.created_at;
      ELSE
        IF (to_jsonb(NEW)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at'])
        OR NEW.updated_at<OLD.updated_at OR NEW.attempt<OLD.attempt OR NEW.fence<OLD.fence OR OLD.delivery_state NOT IN('PENDING','LEASED') THEN RAISE EXCEPTION 'immutable Preference event or terminal control' USING ERRCODE='55000';END IF;
        IF NEW.delivery_state='LEASED' THEN
          PERFORM pg_advisory_xact_lock(hashtextextended(NEW.subject_id::text||':'||NEW.agent_id::text||':'||NEW.root_trace_id::text,76034));
          IF NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1 OR NEW.lease_until<=clock_timestamp() OR NEW.expires_at<=clock_timestamp()
          OR (OLD.delivery_state='LEASED' AND OLD.lease_until>clock_timestamp()) OR (SELECT sum(attempt) FROM agent_domain_outbox WHERE subject_id=NEW.subject_id AND agent_id=NEW.agent_id AND root_trace_id=NEW.root_trace_id AND schema_version=NEW.schema_version)>=6 THEN RAISE EXCEPTION 'shared Preference root attempt limit or lease boundary' USING ERRCODE='55000';END IF;
        ELSIF NEW.fence<>OLD.fence OR NEW.attempt<>OLD.attempt THEN RAISE EXCEPTION 'Preference fencing advances only on claim' USING ERRCODE='55000';END IF;
        IF NEW.delivery_state IN('PENDING','INVALIDATION_COMPLETE') AND (OLD.delivery_state<>'LEASED' OR NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=NEW.event_id AND i.subject_id=NEW.subject_id AND i.handler_version='preference-invalidation-v1' AND i.fence=NEW.fence AND i.attempt=NEW.attempt AND i.control_state=NEW.delivery_state AND i.xmin::text=(txid_current()%4294967296)::text)) THEN RAISE EXCEPTION 'Preference checkpoint requires sameTx progress or completion receipt' USING ERRCODE='55000';END IF;
      END IF;
      RETURN NEW;
    END IF;
    IF NEW.schema_version='agent-memory-outbox-v1' THEN
      IF NEW.delivery_state NOT IN('PENDING','LEASED','INVALIDATED','EXPIRED','DEAD_LETTER','INVALIDATION_COMPLETE') THEN RAISE EXCEPTION 'unsupported Memory control state' USING ERRCODE='55000';END IF;
      IF TG_OP='INSERT' THEN
        IF NEW.delivery_state<>'PENDING' OR NEW.attempt<>0 OR NEW.fence<>0 OR NEW.lease_owner IS NOT NULL OR NEW.lease_until IS NOT NULL OR NEW.received_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp()
        OR NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE a.id=NEW.subject_id AND a.account_type='person' AND a.status='active' AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0)
        OR NOT EXISTS(SELECT 1 FROM agent_memories m WHERE m.id=NEW.source_id AND m.owner_id=NEW.subject_id AND m.agent_id=NEW.agent_id AND m.owner_type='PERSON' AND m.version=NEW.source_revision AND m.status=NEW.source_status AND m.updated_at=NEW.occurred_at AND (m.source_type='EXPLICIT' OR m.status='DELETED') AND birdtie_memory_outbox_fingerprint(m,m.xmin::text)=NEW.source_fingerprint)
        OR NEW.event_id<>birdtie_memory_outbox_event(NEW.event_type,NEW.subject_id,NEW.agent_id,NEW.source_id,NEW.source_revision,NEW.source_status,NEW.source_fingerprint)
        OR NEW.logical_operation_id<>birdtie_memory_outbox_operation(NEW.source_id,NEW.source_revision,NEW.event_type)
        OR NEW.root_trace_id<>birdtie_memory_outbox_operation(NEW.source_id,NEW.source_revision,'MEMORY_UPDATED') THEN RAISE EXCEPTION 'exact native Memory capture required' USING ERRCODE='55000';END IF;
        IF NEW.event_type='MEMORY_UPDATED' THEN
          IF NEW.causation_id IS NOT NULL THEN RAISE EXCEPTION 'Memory mutation root cannot be self-generated' USING ERRCODE='55000';END IF;
        ELSIF NEW.event_type='MEMORY_CONTEXT_INVALIDATION' THEN
          IF NOT EXISTS(SELECT 1 FROM agent_domain_outbox p JOIN agent_consumer_inbox i ON i.event_id=p.event_id AND i.subject_id=p.subject_id
            WHERE p.event_id=NEW.causation_id AND p.event_type='MEMORY_UPDATED' AND p.schema_version=NEW.schema_version AND p.causation_id IS NULL AND p.root_trace_id=NEW.root_trace_id AND p.subject_id=NEW.subject_id AND p.agent_id=NEW.agent_id
            AND p.source_id=NEW.source_id AND p.source_revision=NEW.source_revision AND p.source_fingerprint=NEW.source_fingerprint AND p.source_status=NEW.source_status AND p.occurred_at=NEW.occurred_at AND p.expires_at=NEW.expires_at
            AND p.delivery_state='INVALIDATION_COMPLETE' AND i.handler_version='memory-invalidation-v1' AND i.control_state='INVALIDATION_COMPLETE' AND i.reason_code='CLEANUP_COMPLETE' AND i.fence=p.fence AND i.attempt=p.attempt AND NEW.received_at=i.updated_at AND i.updated_at<=p.updated_at
            AND p.xmin::text=(txid_current()%4294967296)::text AND i.xmin::text=(txid_current()%4294967296)::text) THEN RAISE EXCEPTION 'only sameTx completed root may create fixed depth1 invalidation child' USING ERRCODE='55000';END IF;
        ELSE RAISE EXCEPTION 'Memory causal depth exceeded' USING ERRCODE='55000';END IF;
        NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;NEW.next_attempt_at:=NEW.created_at;
      ELSE
        IF (to_jsonb(NEW)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at'])
        OR NEW.updated_at<OLD.updated_at OR NEW.attempt<OLD.attempt OR NEW.fence<OLD.fence OR OLD.delivery_state NOT IN('PENDING','LEASED') THEN RAISE EXCEPTION 'immutable Memory event or terminal control' USING ERRCODE='55000';END IF;
        IF NEW.delivery_state='LEASED' THEN
          PERFORM pg_advisory_xact_lock(hashtextextended(NEW.subject_id::text||':'||NEW.agent_id::text||':'||NEW.root_trace_id::text,76034));
          IF NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1 OR NEW.lease_until<=clock_timestamp() OR NEW.expires_at<=clock_timestamp()
          OR (OLD.delivery_state='LEASED' AND OLD.lease_until>clock_timestamp()) OR (SELECT sum(attempt) FROM agent_domain_outbox WHERE subject_id=NEW.subject_id AND agent_id=NEW.agent_id AND root_trace_id=NEW.root_trace_id AND schema_version=NEW.schema_version)>=6 THEN RAISE EXCEPTION 'shared Memory root attempt limit or lease boundary' USING ERRCODE='55000';END IF;
        ELSIF NEW.fence<>OLD.fence OR NEW.attempt<>OLD.attempt THEN RAISE EXCEPTION 'Memory fencing advances only on claim' USING ERRCODE='55000';END IF;
        IF NEW.delivery_state IN('PENDING','INVALIDATION_COMPLETE') AND (OLD.delivery_state<>'LEASED' OR NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=NEW.event_id AND i.subject_id=NEW.subject_id AND i.handler_version='memory-invalidation-v1' AND i.fence=NEW.fence AND i.attempt=NEW.attempt AND i.control_state=NEW.delivery_state AND i.xmin::text=(txid_current()%4294967296)::text)) THEN RAISE EXCEPTION 'Memory checkpoint requires sameTx progress or completion receipt' USING ERRCODE='55000';END IF;
      END IF;
      RETURN NEW;
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.delivery_state<>'PENDING' OR NEW.attempt<>0 OR NEW.fence<>0 OR NEW.lease_owner IS NOT NULL OR NEW.lease_until IS NOT NULL
          OR NEW.received_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp()
          OR NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
              JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
              WHERE a.id=NEW.subject_id AND a.account_type='person' AND a.status='active'
              AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active') THEN
            RAISE EXCEPTION 'native outbox capture requires current Person binding and fresh source metadata';
        END IF;
        IF NOT EXISTS(SELECT 1 FROM moments m WHERE m.id=NEW.source_id AND m.author_account_id=NEW.subject_id
            AND m.visibility='private' AND m.revision=NEW.source_revision AND m.status=NEW.source_status
            AND ((NEW.event_type='MOMENT_CREATED' AND m.created_at=NEW.occurred_at)
              OR(NEW.event_type IN('MOMENT_UPDATED','MOMENT_WITHDRAWN') AND m.updated_at=NEW.occurred_at))) THEN
            RAISE EXCEPTION 'outbox capture requires exact current native Moment mutation metadata';
        END IF;
        -- Independent clock_timestamp defaults may differ by microseconds.
        -- Native control timestamps are one real server clock, never authority
        -- or a reason to fabricate/renew the source mutation time.
        NEW.created_at:=clock_timestamp();
        NEW.updated_at:=NEW.created_at;
        NEW.next_attempt_at:=NEW.created_at;
    ELSE
        IF (to_jsonb(NEW)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at'])
           IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at']) THEN
            RAISE EXCEPTION 'outbox event identity and source metadata are immutable';
        END IF;
        IF NEW.updated_at<OLD.updated_at OR NEW.attempt<OLD.attempt OR NEW.fence<OLD.fence THEN
            RAISE EXCEPTION 'outbox controls cannot regress';
        END IF;
        IF NEW.delivery_state='LEASED' THEN
            IF OLD.fence=9223372036854775807 OR NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1
              OR NEW.lease_until<=clock_timestamp() OR NEW.expires_at<=clock_timestamp() THEN
                RAISE EXCEPTION 'outbox lease requires a new fencing token and current deadline';
            END IF;
        ELSIF NEW.fence<>OLD.fence OR NEW.attempt<>OLD.attempt THEN
            RAISE EXCEPTION 'outbox fencing advances only on a claim';
        END IF;
    END IF;
    IF TG_OP='UPDATE' AND OLD.delivery_state='CANDIDATE_STAGED' THEN RAISE EXCEPTION 'committed candidate checkpoint cannot be revived' USING ERRCODE='55000'; END IF;
    IF NEW.delivery_state='CANDIDATE_STAGED' AND (TG_OP<>'UPDATE' OR OLD.delivery_state<>'LEASED' OR NOT EXISTS(
      SELECT 1 FROM agent_effect_ledger e JOIN agent_memory_candidates c ON c.id=e.candidate_id JOIN agent_consumer_inbox i ON i.event_id=e.event_id AND i.subject_id=e.subject_id AND i.handler_version=e.handler_version
      WHERE e.event_id=NEW.event_id AND e.subject_id=NEW.subject_id AND e.agent_id=NEW.agent_id AND e.fence=NEW.fence AND e.attempt=NEW.attempt AND e.logical_operation_id=NEW.logical_operation_id
      AND e.xmin::text=(txid_current()%4294967296)::text AND c.status='CANDIDATE' AND c.valid_until>clock_timestamp() AND c.xmin::text=(txid_current()%4294967296)::text
      AND i.control_state='CANDIDATE_STAGED' AND i.fence=e.fence AND i.attempt=e.attempt AND i.xmin::text=(txid_current()%4294967296)::text AND birdtie_candidate_pipeline_current(e.retention_grant_id))) THEN
      RAISE EXCEPTION 'candidate checkpoint requires current native sameTx candidate/effect/inbox' USING ERRCODE='55000'; END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_guard_agent_consumer_inbox() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent agent_domain_outbox%ROWTYPE;
BEGIN

    IF NEW.handler_version='preference-invalidation-v1' THEN
      SELECT * INTO parent FROM agent_domain_outbox WHERE event_id=NEW.event_id AND subject_id=NEW.subject_id;
      IF parent.schema_version IS DISTINCT FROM 'agent-preference-outbox-v1' OR NEW.fence<>parent.fence OR NEW.attempt<>parent.attempt THEN RAISE EXCEPTION 'Preference receipt native fence mismatch' USING ERRCODE='55000';END IF;
      IF TG_OP='UPDATE' THEN
        IF NEW.event_id<>OLD.event_id OR NEW.subject_id<>OLD.subject_id OR NEW.handler_version<>OLD.handler_version OR NEW.created_at<>OLD.created_at OR NEW.updated_at<OLD.updated_at OR NEW.fence<OLD.fence OR NEW.attempt<OLD.attempt OR OLD.control_state NOT IN('LEASED','PENDING') THEN RAISE EXCEPTION 'Preference receipt identity or terminal cannot revive' USING ERRCODE='55000';END IF;
        IF OLD.control_state='PENDING' AND (NEW.control_state<>'LEASED' OR NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1) THEN RAISE EXCEPTION 'progress requires new fenced claim' USING ERRCODE='55000';END IF;
      ELSIF NEW.control_state<>'LEASED' THEN RAISE EXCEPTION 'Preference initial receipt requires claim' USING ERRCODE='55000';END IF;
      IF parent.delivery_state<>'LEASED' OR parent.lease_until<=clock_timestamp() OR parent.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'Preference receipt requires current lease' USING ERRCODE='55000';END IF;
      RETURN NEW;
    END IF;
    IF NEW.handler_version='memory-invalidation-v1' THEN
      SELECT * INTO parent FROM agent_domain_outbox WHERE event_id=NEW.event_id AND subject_id=NEW.subject_id;
      IF parent.schema_version IS DISTINCT FROM 'agent-memory-outbox-v1' OR NEW.fence<>parent.fence OR NEW.attempt<>parent.attempt THEN RAISE EXCEPTION 'Memory receipt native fence mismatch' USING ERRCODE='55000';END IF;
      IF TG_OP='UPDATE' THEN
        IF NEW.event_id<>OLD.event_id OR NEW.subject_id<>OLD.subject_id OR NEW.handler_version<>OLD.handler_version OR NEW.created_at<>OLD.created_at OR NEW.updated_at<OLD.updated_at OR NEW.fence<OLD.fence OR NEW.attempt<OLD.attempt OR OLD.control_state NOT IN('LEASED','PENDING') THEN RAISE EXCEPTION 'Memory receipt identity or terminal cannot revive' USING ERRCODE='55000';END IF;
        IF OLD.control_state='PENDING' AND (NEW.control_state<>'LEASED' OR NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1) THEN RAISE EXCEPTION 'progress requires new fenced claim' USING ERRCODE='55000';END IF;
      ELSIF NEW.control_state<>'LEASED' THEN RAISE EXCEPTION 'Memory initial receipt requires claim' USING ERRCODE='55000';END IF;
      IF parent.delivery_state<>'LEASED' OR parent.lease_until<=clock_timestamp() OR parent.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'Memory receipt requires current lease' USING ERRCODE='55000';END IF;
      RETURN NEW;
    END IF;
    SELECT * INTO parent FROM agent_domain_outbox WHERE event_id=NEW.event_id AND subject_id=NEW.subject_id;
    IF NOT FOUND OR NEW.fence<>parent.fence OR NEW.attempt<>parent.attempt THEN
        RAISE EXCEPTION 'consumer receipt requires the current native event fence';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.event_id<>OLD.event_id OR NEW.subject_id<>OLD.subject_id OR NEW.handler_version<>OLD.handler_version
          OR NEW.created_at<>OLD.created_at OR NEW.updated_at<OLD.updated_at OR NEW.fence<OLD.fence OR NEW.attempt<OLD.attempt THEN
            RAISE EXCEPTION 'consumer identity and controls cannot regress';
        END IF;
        IF OLD.control_state<>'LEASED' THEN RAISE EXCEPTION 'terminal handler receipt cannot be revived'; END IF;
    END IF;
    IF NEW.control_state='LEASED' AND (parent.delivery_state<>'LEASED' OR parent.lease_until<=clock_timestamp()) THEN
        RAISE EXCEPTION 'consumer lease requires current outbox ownership';
    END IF;
    IF NEW.control_state='CANDIDATE_STAGED' AND (TG_OP<>'UPDATE' OR OLD.control_state<>'LEASED' OR parent.delivery_state<>'LEASED' OR parent.lease_until<=clock_timestamp() OR NOT EXISTS(
      SELECT 1 FROM agent_effect_ledger e JOIN agent_memory_candidates c ON c.id=e.candidate_id
      WHERE e.event_id=NEW.event_id AND e.subject_id=NEW.subject_id AND e.handler_version=NEW.handler_version AND e.fence=NEW.fence AND e.attempt=NEW.attempt
      AND e.xmin::text=(txid_current()%4294967296)::text AND c.xmin::text=(txid_current()%4294967296)::text AND c.status='CANDIDATE' AND c.valid_until>clock_timestamp() AND birdtie_candidate_pipeline_current(e.retention_grant_id))) THEN
       RAISE EXCEPTION 'candidate inbox checkpoint requires current native sameTx candidate/effect' USING ERRCODE='55000'; END IF;
    RETURN NEW;
END $$;
COMMIT;
