BEGIN;
-- Never remove existing native previews/history to make rollback possible.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM model_egress_previews WHERE scope='SELF_TASK_QUERY_LICENSED_PLACES') THEN
  RAISE EXCEPTION 'retained licensed-place records prevent rollback; stop feature instead';
 END IF;
END $$;
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
DROP FUNCTION IF EXISTS birdtie_model_licensed_place_batch_shape(jsonb);
COMMIT;

