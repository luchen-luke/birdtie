BEGIN;
-- Extend original 088 execution metadata. Original 062 remains the sole
-- approval, account, reservation and cash authority. No history is rewritten.
ALTER TABLE model_request_runs ADD COLUMN run_kind text NOT NULL DEFAULT 'LOCAL_RETRY'
 CHECK(run_kind IN ('LOCAL_RETRY','LIVE_SOURCE_ANSWER'));
ALTER TABLE model_request_run_steps
 ADD COLUMN step_kind text NOT NULL DEFAULT 'LOCAL_MODEL' CHECK(step_kind IN ('LOCAL_MODEL','SOURCE_RETRIEVAL','MODEL_INFERENCE')),
 ADD COLUMN binding_state text NOT NULL DEFAULT 'BOUND' CHECK(binding_state IN ('BOUND','WAITING_SOURCE')),
 ADD COLUMN source_evidence_digest text CHECK(source_evidence_digest~'^[0-9a-f]{64}$' AND source_evidence_digest<>repeat('0',64)),
 ADD COLUMN planned_max_output_tokens integer,
 ALTER COLUMN preview_id DROP NOT NULL,
 ALTER COLUMN request_digest DROP NOT NULL;
ALTER TABLE model_request_run_steps ADD CONSTRAINT model_request_live_step_shape CHECK(
 (step_kind='LOCAL_MODEL' AND binding_state='BOUND' AND preview_id IS NOT NULL AND request_digest IS NOT NULL
  AND source_evidence_digest IS NULL AND planned_max_output_tokens IS NULL)
 OR
 (step_kind='SOURCE_RETRIEVAL' AND ordinal=1 AND binding_state='BOUND' AND preview_id IS NOT NULL
  AND request_digest IS NOT NULL AND source_evidence_digest IS NULL AND planned_max_output_tokens IS NOT NULL AND planned_max_output_tokens=0)
 OR
 (step_kind='MODEL_INFERENCE' AND ordinal=2 AND planned_max_output_tokens IS NOT NULL AND planned_max_output_tokens BETWEEN 1 AND 768
  AND ((binding_state='WAITING_SOURCE' AND state='PLANNED' AND preview_id IS NULL AND request_digest IS NULL
        AND source_evidence_digest IS NULL AND reservation_id IS NULL)
    OR (binding_state='BOUND' AND preview_id IS NOT NULL AND request_digest IS NOT NULL AND source_evidence_digest IS NOT NULL)))
);

CREATE FUNCTION birdtie_model_request_live_step_reference() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE run model_request_runs%ROWTYPE; preview model_egress_previews%ROWTYPE; price_kind text;
BEGIN
 SELECT * INTO STRICT run FROM model_request_runs WHERE id=NEW.run_id;
 SELECT billing_kind INTO price_kind FROM model_local_price_versions WHERE version=NEW.price_version;
 IF run.run_kind='LOCAL_RETRY' THEN
  IF NEW.step_kind<>'LOCAL_MODEL' OR price_kind<>'LOCAL' THEN RAISE EXCEPTION 'local run requires local steps'; END IF;
 ELSE
  IF NOT ((NEW.ordinal=1 AND NEW.step_kind='SOURCE_RETRIEVAL' AND price_kind='CALL')
    OR (NEW.ordinal=2 AND NEW.step_kind='MODEL_INFERENCE' AND price_kind='TOKEN')) THEN
   RAISE EXCEPTION 'typed original source pipeline required'; END IF;
  IF TG_OP='INSERT' AND (NEW.state<>'PLANNED' OR (NEW.ordinal=2 AND NEW.binding_state<>'WAITING_SOURCE')) THEN
   RAISE EXCEPTION 'typed pipeline must begin unreserved and unbound'; END IF;
 END IF;
 IF NEW.preview_id IS NOT NULL THEN
  SELECT * INTO STRICT preview FROM model_egress_previews WHERE id=NEW.preview_id;
  IF preview.billing_kind IS DISTINCT FROM price_kind OR preview.price_version<>NEW.price_version
   OR preview.request_digest<>NEW.request_digest OR preview.owner_id<>run.owner_id
   OR preview.session_id<>run.session_id OR preview.task_id<>run.task_id OR preview.root_trace_id<>run.root_trace_id
   OR preview.binding_id<>run.binding_id OR preview.source_token<>run.source_token OR preview.authority_token<>run.authority_token THEN
   RAISE EXCEPTION 'step must retain exact original preview'; END IF;
  IF run.run_kind='LIVE_SOURCE_ANSWER' AND (TG_OP='INSERT' OR OLD.binding_state='WAITING_SOURCE') AND (preview.status<>'APPROVED' OR preview.expires_at<run.deadline_at
   OR (NEW.ordinal=1 AND preview.scope<>'SELF_TASK_QUERY')
   OR (NEW.ordinal=2 AND (preview.scope<>'SELF_TASK_QUERY_PUBLIC_SEARCH'
      OR preview.source_evidence_digest IS DISTINCT FROM NEW.source_evidence_digest
      OR preview.max_output_tokens<>NEW.planned_max_output_tokens
      OR NOT EXISTS(SELECT 1 FROM model_request_run_steps first_step JOIN model_budget_reservations r
       ON r.operation_id=first_step.operation_id WHERE first_step.run_id=run.id AND first_step.ordinal=1
       AND first_step.step_kind='SOURCE_RETRIEVAL' AND first_step.state='UNKNOWN'
       AND r.state='UNKNOWN' AND r.execution_status='LIVE_ATTEMPTED' AND r.billing_kind='CALL'
       AND first_step.operation_id::text=preview.source_batch->'evidence'->>'OperationID')))) THEN
   RAISE EXCEPTION 'approved typed source preview required'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_request_live_step_reference BEFORE INSERT OR UPDATE ON model_request_run_steps
 FOR EACH ROW EXECUTE FUNCTION birdtie_model_request_live_step_reference();

CREATE OR REPLACE FUNCTION birdtie_model_request_run_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE late_bind boolean;
BEGIN
 IF TG_TABLE_NAME='model_request_runs' THEN
  IF to_jsonb(NEW)-ARRAY['state','revision','fence','deadline_at','lease_until','updated_at']::text[]<>to_jsonb(OLD)-ARRAY['state','revision','fence','deadline_at','lease_until','updated_at']::text[]
   OR NEW.revision<>OLD.revision+1 OR NEW.fence<OLD.fence
   OR NEW.deadline_at>OLD.deadline_at OR NEW.lease_until>OLD.lease_until OR NEW.updated_at<OLD.updated_at
   OR (OLD.state IN('FINISHED','STOPPED','CANCELLED','EXPIRED') AND NEW.state<>OLD.state)
   OR (OLD.state='PLANNED' AND NEW.state NOT IN('PLANNED','RUNNING','STOPPED','CANCELLED','EXPIRED'))
   OR (OLD.state='RUNNING' AND NEW.state NOT IN('RUNNING','FINISHED','STOPPED','CANCELLED','EXPIRED')) THEN
   RAISE EXCEPTION 'model request run identity or original boundary changed'; END IF;
 ELSE
  late_bind:=OLD.step_kind='MODEL_INFERENCE' AND OLD.binding_state='WAITING_SOURCE' AND NEW.binding_state='BOUND'
   AND OLD.state='PLANNED' AND NEW.state='PLANNED' AND OLD.preview_id IS NULL AND OLD.request_digest IS NULL
   AND OLD.source_evidence_digest IS NULL AND OLD.reservation_id IS NULL AND NEW.reservation_id IS NULL
   AND NEW.preview_id IS NOT NULL AND NEW.request_digest IS NOT NULL AND NEW.source_evidence_digest IS NOT NULL;
  IF late_bind THEN
   IF to_jsonb(NEW)-ARRAY['binding_state','preview_id','request_digest','source_evidence_digest','updated_at']::text[]
    <>to_jsonb(OLD)-ARRAY['binding_state','preview_id','request_digest','source_evidence_digest','updated_at']::text[]
    OR NEW.updated_at<OLD.updated_at OR NOT EXISTS(SELECT 1 FROM model_request_runs r WHERE r.id=NEW.run_id
     AND r.run_kind='LIVE_SOURCE_ANSWER' AND r.state='RUNNING' AND r.deadline_at>clock_timestamp() AND r.lease_until>clock_timestamp()) THEN
    RAISE EXCEPTION 'one exact live source binding required'; END IF;
  ELSE
   IF to_jsonb(NEW)-ARRAY['state','reservation_id','updated_at']::text[]<>to_jsonb(OLD)-ARRAY['state','reservation_id','updated_at']::text[] OR NEW.updated_at<OLD.updated_at
    OR NOT((OLD.state='PLANNED' AND NEW.state='RESERVED') OR (OLD.state='RESERVED' AND NEW.state IN('IN_FLIGHT','CANCELLED_BEFORE_SEND'))
     OR (OLD.state='IN_FLIGHT' AND NEW.state IN('SETTLED','UNKNOWN')) OR (OLD.state='UNKNOWN' AND NEW.state='SETTLED'))
    OR (OLD.reservation_id IS NOT NULL AND NEW.reservation_id IS DISTINCT FROM OLD.reservation_id) THEN
    RAISE EXCEPTION 'exact original model step observation required'; END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
COMMIT;
