BEGIN;
LOCK TABLE agent_domain_outbox,agent_consumer_inbox,agent_effect_ledger,agent_memory_candidates,consent_grants IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_effect_ledger) OR EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2')) OR EXISTS(SELECT 1 FROM agent_domain_outbox WHERE delivery_state='CANDIDATE_STAGED') THEN
 RAISE EXCEPTION '082 down refuses candidate/effect/handler history'; END IF;
END $$;
DROP TRIGGER candidate_pipeline_atomic_final ON agent_effect_ledger;
DROP TRIGGER candidate_pipeline_revoke ON consent_grants;
DROP FUNCTION birdtie_candidate_pipeline_final();
DROP FUNCTION birdtie_candidate_pipeline_revoke();
DROP FUNCTION birdtie_expire_candidate_pipeline(integer);
CREATE OR REPLACE FUNCTION birdtie_agent_effect_writer_unavailable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'actual candidate effect writer and current purpose resolver unavailable' USING ERRCODE='55000';
END $$;
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
    RETURN NEW;
END $$;
DROP FUNCTION birdtie_candidate_pipeline_current(uuid);
DROP INDEX candidate_effect_one_candidate;
ALTER TABLE agent_effect_ledger DROP CONSTRAINT candidate_effect_native_binding,DROP CONSTRAINT candidate_effect_no_persisted_proof,DROP COLUMN candidate_id,DROP COLUMN retention_grant_id,DROP COLUMN event_id,DROP COLUMN handler_version,DROP COLUMN fence,DROP COLUMN attempt,DROP COLUMN proof_review;
ALTER TABLE agent_domain_outbox DROP CONSTRAINT agent_domain_outbox_delivery_state_check;
ALTER TABLE agent_domain_outbox ADD CONSTRAINT agent_domain_outbox_delivery_state_check CHECK(delivery_state IN('PENDING','LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_handler_version_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_handler_version_check CHECK(handler_version IN('mom-control-v1','mom-control-v2'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_control_state_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_control_state_check CHECK(control_state IN('LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_check1;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_check1 CHECK((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED'));
COMMIT;
