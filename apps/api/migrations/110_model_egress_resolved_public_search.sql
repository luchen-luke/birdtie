BEGIN;

-- Extend only the original immutable preview's closed data scopes. The 062
-- approval/accounting ledgers and all 107/109 cash, Run and fence rules remain.
ALTER TABLE model_egress_previews DROP CONSTRAINT model_egress_preview_scope_closed;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_egress_preview_scope_closed CHECK(scope IN (
 'SELF_TASK_QUERY','SELF_TASK_QUERY_PUBLIC_SEARCH',
 'SELF_TASK_PUBLIC_RESOLVED_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES'));

ALTER TABLE model_egress_previews DROP CONSTRAINT model_live_source_export_shape;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_live_source_export_shape CHECK(
 (scope='SELF_TASK_QUERY' AND source_batch IS NULL AND source_evidence_digest IS NULL)
 OR
 (scope='SELF_TASK_PUBLIC_RESOLVED_SEARCH' AND billing_kind='CALL' AND purpose='PUBLIC_SOURCE_RETRIEVAL'
  AND source_batch IS NULL AND source_evidence_digest IS NULL)
 OR
 (scope IN ('SELF_TASK_QUERY_PUBLIC_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES')
  AND billing_kind='TOKEN' AND purpose='MODEL_CONTEXT_EGRESS'
  AND source_batch IS NOT NULL AND source_evidence_digest IS NOT NULL
  AND source_evidence_digest~'^[0-9a-f]{64}$' AND source_evidence_digest<>repeat('0',64)
  AND octet_length(source_batch::text)<=16384
  AND birdtie_model_live_source_batch_shape(source_batch)
  AND source_batch->'evidence'->>'QueryEvidenceDigest'=current_query_digest)
);

-- The original ten evidence fields already bind the exact compiled CALL
-- request/payload and raw current query. No new reconstructible grant is added.
CREATE OR REPLACE FUNCTION birdtie_model_live_source_reference_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_operation uuid; call_preview model_egress_previews%ROWTYPE;
 attempt model_budget_reservations%ROWTYPE; observed timestamptz; deadline timestamptz;
 expected_call_scope text;
BEGIN
 IF NEW.scope IN ('SELF_TASK_QUERY','SELF_TASK_PUBLIC_RESOLVED_SEARCH') THEN RETURN NEW; END IF;
 IF NEW.scope='SELF_TASK_QUERY_PUBLIC_SEARCH' THEN expected_call_scope:='SELF_TASK_QUERY';
 ELSIF NEW.scope='SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES' THEN expected_call_scope:='SELF_TASK_PUBLIC_RESOLVED_SEARCH';
 ELSE RAISE EXCEPTION 'invalid source export scope'; END IF;
 IF NOT birdtie_model_live_source_batch_shape(NEW.source_batch) THEN
  RAISE EXCEPTION 'invalid source export shape'; END IF;
 source_operation:=(NEW.source_batch->'evidence'->>'OperationID')::uuid;
 observed:=(NEW.source_batch->'evidence'->>'ObservedAt')::timestamptz;
 deadline:=(NEW.source_batch->'evidence'->>'DeadlineAt')::timestamptz;
 SELECT * INTO attempt FROM model_budget_reservations WHERE operation_id=source_operation FOR SHARE;
 SELECT * INTO call_preview FROM model_egress_previews WHERE id=attempt.preview_id FOR SHARE;
 IF attempt.operation_id IS NULL OR call_preview.id IS NULL
  OR attempt.billing_kind<>'CALL' OR attempt.state<>'UNKNOWN'
  OR attempt.execution_status<>'LIVE_ATTEMPTED' OR attempt.cash_status<>'UNKNOWN'
  OR call_preview.billing_kind<>'CALL' OR call_preview.scope<>expected_call_scope
  OR call_preview.status<>'APPROVED' OR call_preview.source_batch IS NOT NULL
  OR call_preview.id::text<>NEW.source_batch->'evidence'->>'PreviewID'
  OR call_preview.request_digest<>NEW.source_batch->'evidence'->>'SourceRequestDigest'
  OR call_preview.egress_payload_digest<>NEW.source_batch->'evidence'->>'SourcePayloadDigest'
  OR call_preview.current_query_digest<>NEW.current_query_digest
  OR attempt.request_digest<>call_preview.request_digest
  OR attempt.owner_id<>NEW.owner_id OR call_preview.owner_id<>NEW.owner_id
  OR attempt.root_trace_id<>NEW.root_trace_id OR call_preview.root_trace_id<>NEW.root_trace_id
  OR attempt.task_id<>NEW.task_id OR call_preview.task_id<>NEW.task_id
  OR call_preview.session_id<>NEW.session_id OR call_preview.agent_id<>NEW.agent_id
  OR call_preview.binding_id<>NEW.binding_id OR call_preview.source_token<>NEW.source_token
  OR call_preview.authority_token<>NEW.authority_token OR call_preview.expires_at<>deadline
  OR NOT isfinite(observed) OR NOT isfinite(deadline) OR observed<attempt.created_at
  OR observed>NEW.created_at OR observed>=deadline OR deadline<=NEW.created_at
  OR NEW.expires_at>deadline THEN RAISE EXCEPTION 'source export reference mismatch'; END IF;
 RETURN NEW;
END $$;

-- Preserve the original typed Run reference checks and admit only the new
-- matching scope pair. The original monotonic fence/late-bind guard is intact.
CREATE OR REPLACE FUNCTION birdtie_model_request_live_step_reference() RETURNS trigger LANGUAGE plpgsql AS $$
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
   OR (NEW.ordinal=1 AND preview.scope NOT IN ('SELF_TASK_QUERY','SELF_TASK_PUBLIC_RESOLVED_SEARCH'))
   OR (NEW.ordinal=2 AND (preview.scope NOT IN ('SELF_TASK_QUERY_PUBLIC_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES')
      OR preview.source_evidence_digest IS DISTINCT FROM NEW.source_evidence_digest
      OR preview.max_output_tokens<>NEW.planned_max_output_tokens
      OR NOT EXISTS(SELECT 1 FROM model_request_run_steps first_step JOIN model_budget_reservations r
       ON r.operation_id=first_step.operation_id JOIN model_egress_previews first_preview ON first_preview.id=first_step.preview_id
       WHERE first_step.run_id=run.id AND first_step.ordinal=1
       AND first_step.step_kind='SOURCE_RETRIEVAL' AND first_step.state='UNKNOWN'
       AND r.state='UNKNOWN' AND r.execution_status='LIVE_ATTEMPTED' AND r.billing_kind='CALL'
       AND first_preview.scope=CASE preview.scope WHEN 'SELF_TASK_QUERY_PUBLIC_SEARCH' THEN 'SELF_TASK_QUERY' ELSE 'SELF_TASK_PUBLIC_RESOLVED_SEARCH' END
       AND first_step.operation_id::text=preview.source_batch->'evidence'->>'OperationID')))) THEN
   RAISE EXCEPTION 'approved typed source preview required'; END IF;
 END IF;
 RETURN NEW;
END $$;

COMMIT;
