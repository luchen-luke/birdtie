BEGIN;
-- AIR017: original five automatic attempts plus one explicitly reviewed child.
-- No backfill/update: original FAILED history and xmin remain unchanged.
LOCK TABLE agent_enrichment_runs,agent_run_steps,agent_run_dispatches,agent_run_audit IN ACCESS EXCLUSIVE MODE;
ALTER TABLE agent_enrichment_runs
 ADD COLUMN recovery_root_id uuid REFERENCES agent_enrichment_runs(id) ON DELETE CASCADE,
 ADD COLUMN generation bigint NOT NULL DEFAULT 0,
 ADD COLUMN recovery_reason text,
 ADD COLUMN recovery_previous_version bigint,
 ADD CONSTRAINT agent_run_recovery_shape CHECK(
  (generation=0 AND recovery_root_id IS NULL AND recovery_reason IS NULL AND recovery_previous_version IS NULL)
  OR (generation=1 AND recovery_root_id IS NOT NULL AND recovery_root_id<>id
      AND recovery_reason IS NOT NULL AND recovery_reason='RETRY_TRANSIENT_NO_EFFECT'
      AND recovery_previous_version IS NOT NULL AND recovery_previous_version>0 AND attempt<=1));
ALTER TABLE agent_enrichment_runs DROP CONSTRAINT agent_enrichment_runs_owner_id_agent_id_logical_operation_i_key;
ALTER TABLE agent_enrichment_runs ADD CONSTRAINT agent_run_logical_generation_unique UNIQUE(owner_id,agent_id,logical_operation_id,generation);
CREATE UNIQUE INDEX agent_run_one_live_generation ON agent_enrichment_runs((coalesce(recovery_root_id,id)))
 WHERE state IN('QUEUED','RUNNING','WAITING_CONFIRMATION','RETRY_WAIT');
ALTER TABLE agent_enrichment_runs DROP CONSTRAINT agent_enrichment_runs_reason_check;
ALTER TABLE agent_enrichment_runs ADD CONSTRAINT agent_enrichment_runs_reason_check CHECK(reason IN('WAITING_APPROVAL','QUEUED','DISPATCHING','RECONCILING','COMMITTED','CANCELLED','EXPIRED','RETRY_BUSY','RETRY_UNAVAILABLE','AUTHORITY_CHANGED','ATTEMPTS_EXHAUSTED','INVALID_INPUT'));
CREATE OR REPLACE FUNCTION birdtie_agent_run_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent agent_enrichment_runs%ROWTYPE; total bigint;
BEGIN
 IF TG_OP='UPDATE' AND ROW(NEW.recovery_root_id,NEW.generation,NEW.recovery_reason,NEW.recovery_previous_version)
  IS DISTINCT FROM ROW(OLD.recovery_root_id,OLD.generation,OLD.recovery_reason,OLD.recovery_previous_version) THEN
  RAISE EXCEPTION 'recovery identity is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.generation=1 THEN
  SELECT * INTO STRICT parent FROM agent_enrichment_runs WHERE id=NEW.recovery_root_id FOR UPDATE NOWAIT;
  IF parent.generation<>0 OR parent.state<>'FAILED' OR parent.reason<>'ATTEMPTS_EXHAUSTED'
   OR NEW.owner_id<>parent.owner_id OR NEW.agent_id<>parent.agent_id OR NEW.session_id<>parent.session_id
   OR NEW.authority_binding<>parent.authority_binding OR NEW.source_id<>parent.source_id
   OR NEW.source_revision<>parent.source_revision OR NEW.event_id<>parent.event_id
   OR NEW.logical_operation_id<>parent.logical_operation_id OR NEW.retention_grant_id IS DISTINCT FROM parent.retention_grant_id
   OR NEW.recovery_previous_version<>parent.version OR NEW.deadline>parent.deadline
   OR NEW.attempt>1 OR NEW.recovery_reason<>'RETRY_TRANSIENT_NO_EFFECT' THEN
   RAISE EXCEPTION 'recovery must preserve original authority and deadline' USING ERRCODE='23514';
  END IF;
  SELECT parent.attempt+coalesce(sum(attempt),0) INTO total FROM agent_enrichment_runs
   WHERE recovery_root_id=parent.id AND id<>NEW.id;
  IF total+NEW.attempt>6 THEN RAISE EXCEPTION 'root attempt limit exhausted' USING ERRCODE='23514'; END IF;
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.state NOT IN('QUEUED','WAITING_CONFIRMATION') OR NEW.version<>1 OR NEW.attempt<>0 OR NEW.fence<>0 OR NEW.checkpoint<>'SCHEDULED' THEN
   RAISE EXCEPTION 'invalid initial runtime metadata' USING ERRCODE='23514';
  END IF;
 ELSE
  IF OLD.state IN('SUCCEEDED','FAILED','CANCELLED','EXPIRED') OR
   ROW(NEW.id,NEW.owner_id,NEW.agent_id,NEW.session_id,NEW.authority_binding,NEW.source_id,NEW.source_revision,NEW.event_id,NEW.logical_operation_id,NEW.created_at,NEW.deadline)
    IS DISTINCT FROM ROW(OLD.id,OLD.owner_id,OLD.agent_id,OLD.session_id,OLD.authority_binding,OLD.source_id,OLD.source_revision,OLD.event_id,OLD.logical_operation_id,OLD.created_at,OLD.deadline)
   OR NEW.version<>OLD.version+1 OR NEW.attempt<OLD.attempt OR NEW.fence<OLD.fence
   OR (OLD.retention_grant_id IS NOT NULL AND NEW.retention_grant_id IS DISTINCT FROM OLD.retention_grant_id)
   OR (OLD.retention_grant_id IS NULL AND NEW.retention_grant_id IS NOT NULL AND NOT(OLD.state='WAITING_CONFIRMATION' AND NEW.state='QUEUED')) THEN
   RAISE EXCEPTION 'runtime identity, deadline or terminal state cannot change' USING ERRCODE='23514';
  END IF;
  IF NOT(
   (OLD.state='QUEUED' AND NEW.state IN('RUNNING','WAITING_CONFIRMATION','CANCELLED','EXPIRED')) OR
   (OLD.state='RUNNING' AND NEW.state IN('RETRY_WAIT','SUCCEEDED','FAILED','CANCELLED','EXPIRED')) OR
   (OLD.state='WAITING_CONFIRMATION' AND NEW.state IN('QUEUED','CANCELLED','EXPIRED')) OR
   (OLD.state='RETRY_WAIT' AND NEW.state IN('RUNNING','FAILED','CANCELLED','EXPIRED'))
  ) THEN RAISE EXCEPTION 'invalid runtime transition' USING ERRCODE='23514'; END IF;
  IF NEW.state='RUNNING' AND (NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1 OR NEW.lease_until<=clock_timestamp()) THEN
   RAISE EXCEPTION 'runtime claim must advance attempt and fence' USING ERRCODE='23514';
  END IF;
  IF NEW.state='SUCCEEDED' AND NOT EXISTS(
   SELECT 1 FROM agent_effect_ledger e JOIN agent_domain_outbox d ON d.event_id=e.event_id
    JOIN agent_consumer_inbox i ON i.event_id=e.event_id AND i.subject_id=e.subject_id AND i.handler_version=e.handler_version
   WHERE e.subject_id=NEW.owner_id AND e.agent_id=NEW.agent_id AND e.logical_operation_id=NEW.logical_operation_id
    AND e.event_id=NEW.event_id AND e.retention_grant_id=NEW.retention_grant_id
    AND e.candidate_id=NEW.candidate_id AND e.effect_key=NEW.effect_key
    AND e.action_id='519fde3b-cc12-4abc-8c4f-f3227290a815' AND e.effect_kind='MEMORY_CANDIDATE'
    AND d.delivery_state='CANDIDATE_STAGED' AND i.control_state='CANDIDATE_STAGED'
  ) THEN RAISE EXCEPTION 'runtime success requires original candidate effect' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
COMMIT;
