-- AIR020 native Task lifecycle: capture metadata and narrow prior 076 grants.
-- Source revision=0 is a closed sentinel, never an invented Task counter.
BEGIN;
LOCK TABLE agent_tasks,agent_domain_outbox,agent_consumer_inbox IN ACCESS EXCLUSIVE MODE;
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_schema_version_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_schema_version_check CHECK(schema_version IN('agent-outbox-v1','agent-memory-outbox-v1','agent-preference-outbox-v1','agent-task-request-outbox-v1'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_event_type_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_event_type_check CHECK(event_type IN('MOMENT_CREATED','MOMENT_UPDATED','MOMENT_WITHDRAWN','MEMORY_UPDATED','MEMORY_CONTEXT_INVALIDATION','PREFERENCE_UPDATED','PREFERENCE_CONTEXT_INVALIDATION','TASK_REQUEST_CHANGED','TASK_CONTEXT_INVALIDATION'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_type_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_type_check CHECK(source_type IN('MOMENT','MEMORY','PRIVATE_PREFERENCE','TASK_REQUEST'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_status_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_status_check CHECK(source_status IN('draft','withdrawn','ACTIVE','DELETED','configured','cleared','COMPLETED','FAILED'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_revision_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_revision_check CHECK((schema_version='agent-task-request-outbox-v1' AND source_revision=0) OR(schema_version<>'agent-task-request-outbox-v1' AND source_revision>0));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_handler_version_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_handler_version_check CHECK(handler_version IN('mom-control-v1','mom-control-v2','mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1','memory-invalidation-v1','preference-invalidation-v1','task-request-invalidation-v1'));
DO $$ DECLARE c text; BEGIN
 SELECT conname INTO STRICT c FROM pg_constraint WHERE conrelid='agent_domain_outbox'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%event_type%' AND pg_get_constraintdef(oid) LIKE '%source_status%';
 EXECUTE format('ALTER TABLE agent_domain_outbox DROP CONSTRAINT %I',c);
 EXECUTE format('ALTER TABLE agent_domain_outbox ADD CONSTRAINT %I CHECK(%s)',c,$expr$(schema_version='agent-outbox-v1' AND source_type='MOMENT' AND ((event_type='MOMENT_CREATED' AND source_status='draft' AND source_revision=1) OR(event_type='MOMENT_UPDATED' AND source_status='draft' AND source_revision>1) OR(event_type='MOMENT_WITHDRAWN' AND source_status='withdrawn' AND source_revision>1))) OR(schema_version='agent-memory-outbox-v1' AND source_type='MEMORY' AND event_type IN('MEMORY_UPDATED','MEMORY_CONTEXT_INVALIDATION') AND source_status IN('ACTIVE','DELETED')) OR(schema_version='agent-preference-outbox-v1' AND source_type='PRIVATE_PREFERENCE' AND source_id=agent_id AND source_revision>=2 AND event_type IN('PREFERENCE_UPDATED','PREFERENCE_CONTEXT_INVALIDATION') AND source_status IN('configured','cleared')) OR(schema_version='agent-task-request-outbox-v1' AND source_type='TASK_REQUEST' AND source_revision=0 AND event_type IN('TASK_REQUEST_CHANGED','TASK_CONTEXT_INVALIDATION') AND source_status IN('ACTIVE','COMPLETED','FAILED'))$expr$);
END $$;
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_check1;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_check1 CHECK((handler_version NOT IN('memory-invalidation-v1','preference-invalidation-v1','task-request-invalidation-v1') AND ((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED') OR(control_state='CANDIDATE_STAGED' AND reason_code='CANDIDATE_STAGED' AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1')))) OR(handler_version IN('memory-invalidation-v1','preference-invalidation-v1','task-request-invalidation-v1') AND ((control_state='LEASED' AND reason_code='') OR(control_state='PENDING' AND reason_code='CLEANUP_MORE') OR(control_state='INVALIDATION_COMPLETE' AND reason_code='CLEANUP_COMPLETE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code IN('ROOT_BUDGET_EXHAUSTED','ATTEMPTS_EXHAUSTED')))));

CREATE FUNCTION birdtie_task_request_outbox_operation(t uuid,fp text,k text) RETURNS uuid LANGUAGE sql IMMUTABLE AS $$
 SELECT birdtie_memory_outbox_uuid(convert_to('birdtie.task-request.operation.v1','UTF8')||decode('00','hex')||convert_to(t::text,'UTF8')||decode('00','hex')||convert_to(fp,'UTF8')||decode('00','hex')||convert_to(k,'UTF8'))
$$;
-- This is a separate event namespace. Go reconstructs the 076 purpose token
-- with its exact original compact jsonb projection; these hashes never equate.
CREATE FUNCTION birdtie_task_request_outbox_fingerprint(t agent_tasks,epoch text) RETURNS text LANGUAGE sql STABLE SET TimeZone='UTC' AS $$
 SELECT encode(sha256(convert_to('birdtie.task-request-source.v1'||chr(10)||jsonb_build_object('id',t.id,'owner',t.owner_account_id,'city',t.city_context_id,'context',t.context_id,'status',t.status,'intent',t.intent,'queryDigest',encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex'),'updatedAt',t.updated_at,'row',epoch)::text,'UTF8')),'hex')
$$;
CREATE FUNCTION birdtie_task_request_outbox_event(k text,o uuid,a uuid,t uuid,st text,fp text) RETURNS uuid LANGUAGE sql IMMUTABLE AS $$
 SELECT birdtie_memory_outbox_uuid(convert_to('birdtie.outbox.identity.v1','UTF8')||decode('00','hex')||convert_to(
 '{"Schema":"agent-task-request-outbox-v1","Type":"'||k||'","Tenant":{"type":"PERSON","id":"'||o::text||'"},"Subject":{"type":"PERSON","id":"'||o::text||'"},"Agent":"'||a::text||'","Operation":"'||birdtie_task_request_outbox_operation(t,fp,k)::text||'","Source":{"type":"TASK_REQUEST","id":"'||t::text||'","owner":{"type":"PERSON","id":"'||o::text||'"},"revision":0,"status":"'||st||'","fingerprint":"'||fp||'"}}','UTF8'))
$$;
CREATE FUNCTION birdtie_task_request_outbox_current(tid uuid,o uuid,st text,at timestamptz,fp text) RETURNS boolean LANGUAGE sql VOLATILE SET TimeZone='UTC' AS $$
 SELECT EXISTS(SELECT 1 FROM agent_tasks t WHERE t.id=tid AND t.owner_account_id=o AND t.principal_type='person' AND t.acting_user_account_id=o
 AND t.status=st AND t.updated_at=at AND birdtie_task_request_outbox_fingerprint(t,t.xmin::text)=fp)
$$;
CREATE FUNCTION birdtie_capture_task_request_outbox() RETURNS trigger LANGUAGE plpgsql SET TimeZone='UTC' AS $$
DECLARE a uuid;epoch text;fp text;op uuid;n timestamptz;
BEGIN
 IF NEW.principal_type<>'person' OR NEW.acting_user_account_id IS DISTINCT FROM NEW.owner_account_id THEN RETURN NEW;END IF;
 -- No new binding/cleanup row lock after the source writer's Task lock. The
 -- original consumer will recheck its current native binding before cleanup.
 SELECT ag.id INTO a FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=ac.id AND ap.owner_type='PERSON'
 WHERE ac.id=NEW.owner_account_id AND ac.account_type='person' AND ac.status='active' AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=ac.id AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id);
 IF a IS NULL THEN RETURN NEW;END IF;
 -- Even a same-value UPDATE has a new native xmin and invalidates 076. An
 -- exact Store retry performs no UPDATE, so naturally has no new capture.
 SELECT xmin::text INTO STRICT epoch FROM agent_tasks WHERE id=NEW.id;
 fp:=birdtie_task_request_outbox_fingerprint(NEW,epoch);op:=birdtie_task_request_outbox_operation(NEW.id,fp,'TASK_REQUEST_CHANGED');n:=clock_timestamp();
 INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,occurred_at,received_at,expires_at)
 VALUES('agent-task-request-outbox-v1',birdtie_task_request_outbox_event('TASK_REQUEST_CHANGED',NEW.owner_account_id,a,NEW.id,NEW.status,fp),'TASK_REQUEST_CHANGED',NEW.owner_account_id,NEW.owner_account_id,NEW.owner_account_id,a,'TASK_REQUEST',NEW.id,0,fp,NEW.status,op,op,NEW.updated_at,n,NEW.updated_at+interval '15 minutes')
 ON CONFLICT(event_id) DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER task_request_outbox_capture AFTER INSERT OR UPDATE ON agent_tasks FOR EACH ROW EXECUTE FUNCTION birdtie_capture_task_request_outbox();
CREATE UNIQUE INDEX task_request_outbox_logical_mutation ON agent_domain_outbox(subject_id,agent_id,source_id,source_fingerprint,event_type) WHERE schema_version='agent-task-request-outbox-v1';
CREATE INDEX task_request_outbox_root_budget ON agent_domain_outbox(subject_id,agent_id,root_trace_id) WHERE schema_version='agent-task-request-outbox-v1';

CREATE OR REPLACE FUNCTION birdtie_guard_agent_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.schema_version='agent-task-request-outbox-v1' THEN
      IF NEW.delivery_state NOT IN('PENDING','LEASED','INVALIDATED','EXPIRED','DEAD_LETTER','INVALIDATION_COMPLETE') THEN RAISE EXCEPTION 'unsupported Task request control state' USING ERRCODE='55000';END IF;
      IF TG_OP='INSERT' THEN
        IF NEW.delivery_state<>'PENDING' OR NEW.attempt<>0 OR NEW.fence<>0 OR NEW.lease_owner IS NOT NULL OR NEW.lease_until IS NOT NULL OR NEW.received_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp()
        OR NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE a.id=NEW.subject_id AND a.account_type='person' AND a.status='active' AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0)
        OR NEW.source_revision<>0 OR NOT birdtie_task_request_outbox_current(NEW.source_id,NEW.subject_id,NEW.source_status,NEW.occurred_at,NEW.source_fingerprint)
        OR NEW.event_id<>birdtie_task_request_outbox_event(NEW.event_type,NEW.subject_id,NEW.agent_id,NEW.source_id,NEW.source_status,NEW.source_fingerprint)
        OR NEW.logical_operation_id<>birdtie_task_request_outbox_operation(NEW.source_id,NEW.source_fingerprint,NEW.event_type)
        OR NEW.root_trace_id<>birdtie_task_request_outbox_operation(NEW.source_id,NEW.source_fingerprint,'TASK_REQUEST_CHANGED') THEN RAISE EXCEPTION 'exact native Task request capture required' USING ERRCODE='55000';END IF;
        IF NEW.event_type='TASK_REQUEST_CHANGED' THEN
          IF NEW.causation_id IS NOT NULL THEN RAISE EXCEPTION 'Task request mutation root cannot be self-generated' USING ERRCODE='55000';END IF;
        ELSIF NEW.event_type='TASK_CONTEXT_INVALIDATION' THEN
          IF NOT EXISTS(SELECT 1 FROM agent_domain_outbox p JOIN agent_consumer_inbox i ON i.event_id=p.event_id AND i.subject_id=p.subject_id
            WHERE p.event_id=NEW.causation_id AND p.event_type='TASK_REQUEST_CHANGED' AND p.schema_version=NEW.schema_version AND p.causation_id IS NULL AND p.root_trace_id=NEW.root_trace_id AND p.subject_id=NEW.subject_id AND p.agent_id=NEW.agent_id
            AND p.source_id=NEW.source_id AND p.source_revision=NEW.source_revision AND p.source_fingerprint=NEW.source_fingerprint AND p.source_status=NEW.source_status AND p.occurred_at=NEW.occurred_at AND p.expires_at=NEW.expires_at
            AND p.delivery_state='INVALIDATION_COMPLETE' AND i.handler_version='task-request-invalidation-v1' AND i.control_state='INVALIDATION_COMPLETE' AND i.reason_code='CLEANUP_COMPLETE' AND i.fence=p.fence AND i.attempt=p.attempt AND NEW.received_at=i.updated_at AND i.updated_at<=p.updated_at
            AND p.xmin::text=(txid_current()%4294967296)::text AND i.xmin::text=(txid_current()%4294967296)::text) THEN RAISE EXCEPTION 'only sameTx completed root may create fixed depth1 invalidation child' USING ERRCODE='55000';END IF;
        ELSE RAISE EXCEPTION 'Task request causal depth exceeded' USING ERRCODE='55000';END IF;
        NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;NEW.next_attempt_at:=NEW.created_at;
      ELSE
        IF (to_jsonb(NEW)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['delivery_state','attempt','fence','lease_owner','lease_until','next_attempt_at','updated_at'])
        OR NEW.updated_at<OLD.updated_at OR NEW.attempt<OLD.attempt OR NEW.fence<OLD.fence OR OLD.delivery_state NOT IN('PENDING','LEASED') THEN RAISE EXCEPTION 'immutable Task request event or terminal control' USING ERRCODE='55000';END IF;
        IF NEW.delivery_state='LEASED' THEN
          PERFORM pg_advisory_xact_lock(hashtextextended(NEW.subject_id::text||':'||NEW.agent_id::text||':'||NEW.root_trace_id::text,76034));
          IF NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1 OR NEW.lease_until<=clock_timestamp() OR NEW.expires_at<=clock_timestamp()
          OR (OLD.delivery_state='LEASED' AND OLD.lease_until>clock_timestamp()) OR (SELECT sum(attempt) FROM agent_domain_outbox WHERE subject_id=NEW.subject_id AND agent_id=NEW.agent_id AND root_trace_id=NEW.root_trace_id AND schema_version=NEW.schema_version)>=6 THEN RAISE EXCEPTION 'shared Task request root attempt limit or lease boundary' USING ERRCODE='55000';END IF;
        ELSIF NEW.fence<>OLD.fence OR NEW.attempt<>OLD.attempt THEN RAISE EXCEPTION 'Task request fencing advances only on claim' USING ERRCODE='55000';END IF;
        IF NEW.delivery_state IN('PENDING','INVALIDATION_COMPLETE') AND (OLD.delivery_state<>'LEASED' OR NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=NEW.event_id AND i.subject_id=NEW.subject_id AND i.handler_version='task-request-invalidation-v1' AND i.fence=NEW.fence AND i.attempt=NEW.attempt AND i.control_state=NEW.delivery_state AND i.xmin::text=(txid_current()%4294967296)::text)) THEN RAISE EXCEPTION 'Task request checkpoint requires sameTx progress or completion receipt' USING ERRCODE='55000';END IF;
      END IF;
      RETURN NEW;
    END IF;

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
    IF NEW.handler_version='task-request-invalidation-v1' THEN
      SELECT * INTO parent FROM agent_domain_outbox WHERE event_id=NEW.event_id AND subject_id=NEW.subject_id;
      IF parent.schema_version IS DISTINCT FROM 'agent-task-request-outbox-v1' OR NEW.fence<>parent.fence OR NEW.attempt<>parent.attempt THEN RAISE EXCEPTION 'Task request receipt native fence mismatch' USING ERRCODE='55000';END IF;
      IF TG_OP='UPDATE' THEN
        IF NEW.event_id<>OLD.event_id OR NEW.subject_id<>OLD.subject_id OR NEW.handler_version<>OLD.handler_version OR NEW.created_at<>OLD.created_at OR NEW.updated_at<OLD.updated_at OR NEW.fence<OLD.fence OR NEW.attempt<OLD.attempt OR OLD.control_state NOT IN('LEASED','PENDING') THEN RAISE EXCEPTION 'Task request receipt identity or terminal cannot revive' USING ERRCODE='55000';END IF;
        IF OLD.control_state='PENDING' AND (NEW.control_state<>'LEASED' OR NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1) THEN RAISE EXCEPTION 'progress requires new fenced claim' USING ERRCODE='55000';END IF;
      ELSIF NEW.control_state<>'LEASED' THEN RAISE EXCEPTION 'Task request initial receipt requires claim' USING ERRCODE='55000';END IF;
      IF parent.delivery_state<>'LEASED' OR parent.lease_until<=clock_timestamp() OR parent.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'Task request receipt requires current lease' USING ERRCODE='55000';END IF;
      RETURN NEW;
    END IF;

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
