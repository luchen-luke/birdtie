BEGIN;
LOCK TABLE agent_memories,agent_domain_outbox,agent_consumer_inbox IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_domain_outbox WHERE schema_version='agent-memory-outbox-v1') OR EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE handler_version='memory-invalidation-v1') THEN RAISE EXCEPTION '101 down refuses retained Memory events/progress/receipts' USING ERRCODE='55000';END IF;
END $$;
DROP TRIGGER memory_outbox_capture ON agent_memories;
DROP FUNCTION birdtie_capture_memory_outbox();
DROP INDEX memory_outbox_logical_mutation;
DROP INDEX memory_outbox_root_budget;
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_schema_version_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_schema_version_check CHECK(schema_version='agent-outbox-v1');
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_event_type_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_event_type_check CHECK(event_type IN('MOMENT_CREATED','MOMENT_UPDATED','MOMENT_WITHDRAWN'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_type_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_type_check CHECK(source_type='MOMENT');
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_source_status_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_source_status_check CHECK(source_status IN('draft','withdrawn'));
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_delivery_state_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_delivery_state_check CHECK(delivery_state IN('PENDING','LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER','CANDIDATE_STAGED'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_handler_version_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_handler_version_check CHECK(handler_version IN('mom-control-v1','mom-control-v2','mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_control_state_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_control_state_check CHECK(control_state IN('LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER','CANDIDATE_STAGED'));
DO $$ DECLARE c text; BEGIN
 SELECT conname INTO STRICT c FROM pg_constraint WHERE conrelid='agent_domain_outbox'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%event_type%' AND pg_get_constraintdef(oid) LIKE '%source_status%';
 EXECUTE format('ALTER TABLE agent_domain_outbox DROP CONSTRAINT %I',c);
 EXECUTE format('ALTER TABLE agent_domain_outbox ADD CONSTRAINT %I CHECK(%s)',c,$expr$(event_type='MOMENT_CREATED' AND source_status='draft' AND source_revision=1) OR(event_type='MOMENT_UPDATED' AND source_status='draft' AND source_revision>1) OR(event_type='MOMENT_WITHDRAWN' AND source_status='withdrawn' AND source_revision>1)$expr$);
END $$;
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_check1;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_check1 CHECK((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED') OR(control_state='CANDIDATE_STAGED' AND reason_code='CANDIDATE_STAGED' AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1')));
CREATE OR REPLACE FUNCTION birdtie_guard_agent_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
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
DROP FUNCTION birdtie_memory_outbox_event(text,uuid,uuid,uuid,bigint,text,text);
DROP FUNCTION birdtie_memory_outbox_fingerprint(agent_memories,text);
DROP FUNCTION birdtie_memory_outbox_operation(uuid,bigint,text);
DROP FUNCTION birdtie_memory_outbox_uuid(bytea);
COMMIT;
