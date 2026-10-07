BEGIN;

-- Metadata capture/control only. No user body, media, location, session token,
-- analysis consent, candidate or provider is introduced. Existing sources and
-- versions are neither backfilled nor updated.
CREATE TABLE agent_domain_outbox (
    event_id uuid PRIMARY KEY,
    schema_version text NOT NULL DEFAULT 'agent-outbox-v1' CHECK(schema_version='agent-outbox-v1'),
    event_type text NOT NULL CHECK(event_type IN('MOMENT_CREATED','MOMENT_UPDATED','MOMENT_WITHDRAWN')),
    tenant_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    subject_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    source_type text NOT NULL DEFAULT 'MOMENT' CHECK(source_type='MOMENT'),
    source_id uuid NOT NULL,
    source_revision bigint NOT NULL CHECK(source_revision>0),
    source_fingerprint text NOT NULL CHECK(source_fingerprint ~ '^[0-9a-f]{64}$'),
    source_status text NOT NULL CHECK(source_status IN('draft','withdrawn')),
    logical_operation_id uuid NOT NULL,
    root_trace_id uuid NOT NULL,
    causation_id uuid,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    delivery_state text NOT NULL DEFAULT 'PENDING' CHECK(delivery_state IN('PENDING','LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER')),
    attempt bigint NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 20),
    fence bigint NOT NULL DEFAULT 0 CHECK(fence>=attempt),
    lease_owner uuid,
    lease_until timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(event_id,subject_id),
    CHECK(tenant_id=subject_id AND actor_id=subject_id),
    CHECK(event_id<>'00000000-0000-0000-0000-000000000000' AND subject_id<>'00000000-0000-0000-0000-000000000000'
      AND agent_id<>'00000000-0000-0000-0000-000000000000' AND source_id<>'00000000-0000-0000-0000-000000000000'
      AND logical_operation_id<>'00000000-0000-0000-0000-000000000000' AND root_trace_id<>'00000000-0000-0000-0000-000000000000'
      AND (causation_id IS NULL OR causation_id<>'00000000-0000-0000-0000-000000000000')),
    CHECK((event_type='MOMENT_CREATED' AND source_status='draft' AND source_revision=1)
      OR(event_type='MOMENT_UPDATED' AND source_status='draft' AND source_revision>1)
      OR(event_type='MOMENT_WITHDRAWN' AND source_status='withdrawn' AND source_revision>1)),
    CHECK(isfinite(occurred_at) AND isfinite(received_at) AND isfinite(expires_at)
      AND EXTRACT(YEAR FROM occurred_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND EXTRACT(YEAR FROM received_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND EXTRACT(YEAR FROM expires_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND received_at>=occurred_at AND expires_at=occurred_at+INTERVAL '15 minutes'),
    CHECK(isfinite(created_at) AND isfinite(updated_at) AND isfinite(next_attempt_at)
      AND EXTRACT(YEAR FROM created_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND EXTRACT(YEAR FROM updated_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND EXTRACT(YEAR FROM next_attempt_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND created_at>=received_at AND updated_at>=created_at AND next_attempt_at>=created_at),
    CHECK((delivery_state='LEASED' AND attempt>0 AND fence>0 AND fence<9223372036854775807
      AND lease_owner IS NOT NULL AND lease_owner<>'00000000-0000-0000-0000-000000000000'
      AND lease_until IS NOT NULL AND isfinite(lease_until) AND lease_until>updated_at
      AND lease_until<=updated_at+INTERVAL '30 seconds' AND lease_until<=expires_at)
      OR(delivery_state<>'LEASED' AND lease_owner IS NULL AND lease_until IS NULL))
);
CREATE INDEX agent_outbox_pending ON agent_domain_outbox(next_attempt_at,occurred_at,event_id)
    WHERE delivery_state IN('PENDING','LEASED');

CREATE FUNCTION birdtie_guard_agent_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER agent_outbox_guard BEFORE INSERT OR UPDATE ON agent_domain_outbox
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_outbox();

CREATE TABLE agent_consumer_inbox (
    event_id uuid NOT NULL,
    subject_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    handler_version text NOT NULL CHECK(handler_version IN('mom-control-v1','mom-control-v2')),
    control_state text NOT NULL CHECK(control_state IN('LEASED','UNAVAILABLE','INVALIDATED','EXPIRED','DEAD_LETTER')),
    fence bigint NOT NULL CHECK(fence>0),
    attempt bigint NOT NULL CHECK(attempt BETWEEN 1 AND 20 AND attempt<=fence),
    reason_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(event_id,subject_id,handler_version),
    FOREIGN KEY(event_id,subject_id) REFERENCES agent_domain_outbox(event_id,subject_id) ON DELETE CASCADE,
    CHECK((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE')
      OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED')
      OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED')),
    CHECK(isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at
      AND EXTRACT(YEAR FROM created_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999
      AND EXTRACT(YEAR FROM updated_at AT TIME ZONE 'UTC') BETWEEN 1 AND 9999)
);
CREATE FUNCTION birdtie_guard_agent_consumer_inbox() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER agent_consumer_inbox_guard BEFORE INSERT OR UPDATE ON agent_consumer_inbox
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_consumer_inbox();

-- Stable effects belong to a future real authority writer. This table is an
-- empty scaffold, not a committed candidate/effect and not a second Memory.
CREATE TABLE agent_effect_ledger (
    subject_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    effect_key text NOT NULL CHECK(effect_key ~ '^[0-9a-f]{64}$'),
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    logical_operation_id uuid NOT NULL,
    action_id uuid NOT NULL,
    effect_kind text NOT NULL CHECK(effect_kind='MEMORY_CANDIDATE'),
    action_digest text NOT NULL CHECK(action_digest ~ '^[0-9a-f]{64}$'),
    source_id uuid NOT NULL,
    source_revision bigint NOT NULL CHECK(source_revision>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(subject_id,effect_key)
);
CREATE FUNCTION birdtie_agent_effect_writer_unavailable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'actual candidate effect writer and current purpose resolver unavailable' USING ERRCODE='55000';
END $$;
CREATE TRIGGER agent_effect_writer_unavailable BEFORE INSERT OR UPDATE ON agent_effect_ledger
    FOR EACH ROW EXECUTE FUNCTION birdtie_agent_effect_writer_unavailable();

COMMIT;
