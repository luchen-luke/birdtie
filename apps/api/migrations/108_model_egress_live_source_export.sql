BEGIN;

-- Original 062 preview and reservation ledgers remain the only approval and
-- cash authority. No standalone source grant, account, run or task is added.
ALTER TABLE model_egress_previews
 ADD COLUMN source_batch jsonb,
 ADD COLUMN source_evidence_digest text;
ALTER TABLE model_egress_previews DROP CONSTRAINT model_egress_previews_scope_check;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_egress_preview_scope_closed
 CHECK(scope IN ('SELF_TASK_QUERY','SELF_TASK_QUERY_PUBLIC_SEARCH'));

CREATE FUNCTION birdtie_model_live_source_batch_shape(batch jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE evidence jsonb; item jsonb; k text; value jsonb;
BEGIN
 IF batch IS NULL OR jsonb_typeof(batch)<>'object'
  OR NOT batch ?& ARRAY['evidence','sources']
  OR (SELECT count(*) FROM jsonb_object_keys(batch))<>2 THEN RETURN false; END IF;
 evidence:=batch->'evidence';
 IF jsonb_typeof(evidence)<>'object' OR NOT evidence ?& ARRAY[
  'OperationID','PreviewID','ProviderRequestID','SourceRequestDigest','SourcePayloadDigest',
  'QueryEvidenceDigest','ArtifactSHA256','SelectedSourcesDigest','ObservedAt','DeadlineAt']
  OR (SELECT count(*) FROM jsonb_object_keys(evidence))<>10 THEN RETURN false; END IF;
 FOR k,value IN SELECT * FROM jsonb_each(evidence) LOOP
  IF jsonb_typeof(value)<>'string' THEN RETURN false; END IF;
  IF k IN ('OperationID','PreviewID','ProviderRequestID') THEN
   IF evidence->>k !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    OR evidence->>k='00000000-0000-0000-0000-000000000000' THEN RETURN false; END IF;
  ELSIF k NOT IN ('ObservedAt','DeadlineAt') THEN
   IF evidence->>k !~ '^[0-9a-f]{64}$' OR evidence->>k=repeat('0',64) THEN RETURN false; END IF;
  END IF;
 END LOOP;
 IF jsonb_typeof(batch->'sources')<>'array' THEN RETURN false; END IF;
 IF jsonb_array_length(batch->'sources') NOT BETWEEN 1 AND 10 THEN RETURN false; END IF;
 FOR item IN SELECT * FROM jsonb_array_elements(batch->'sources') LOOP
  IF jsonb_typeof(item)<>'object' OR NOT item ?& ARRAY['title','url','passage']
   OR (SELECT count(*) FROM jsonb_object_keys(item))<>3 THEN RETURN false; END IF;
  IF jsonb_typeof(item->'title')<>'string' OR jsonb_typeof(item->'url')<>'string'
   OR jsonb_typeof(item->'passage')<>'string' THEN RETURN false; END IF;
  IF octet_length(item->>'title') NOT BETWEEN 1 AND 512
   OR octet_length(item->>'url') NOT BETWEEN 1 AND 2048
   OR item->>'url' !~ '^https?://'
   OR octet_length(item->>'passage')>1024 THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END $$;

ALTER TABLE model_egress_previews ADD CONSTRAINT model_live_source_export_shape CHECK(
 (scope='SELF_TASK_QUERY' AND source_batch IS NULL AND source_evidence_digest IS NULL)
 OR
 (scope='SELF_TASK_QUERY_PUBLIC_SEARCH' AND billing_kind='TOKEN' AND purpose='MODEL_CONTEXT_EGRESS'
  AND source_batch IS NOT NULL AND source_evidence_digest IS NOT NULL
  AND source_evidence_digest~'^[0-9a-f]{64}$' AND source_evidence_digest<>repeat('0',64)
  AND octet_length(source_batch::text)<=16384
  AND birdtie_model_live_source_batch_shape(source_batch)
  AND source_batch->'evidence'->>'QueryEvidenceDigest'=current_query_digest)
);

-- This reference guard authenticates native association, not provider success
-- from UNKNOWN. Successful WSA data is minted only by server-local dispatch
-- output. The immutable preview stores its exact selected public evidence.
CREATE FUNCTION birdtie_model_live_source_reference_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_operation uuid; call_preview model_egress_previews%ROWTYPE;
 attempt model_budget_reservations%ROWTYPE; observed timestamptz; deadline timestamptz;
BEGIN
 IF NEW.scope='SELF_TASK_QUERY' THEN RETURN NEW; END IF;
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
  OR call_preview.billing_kind<>'CALL' OR call_preview.scope<>'SELF_TASK_QUERY'
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
CREATE TRIGGER model_live_source_reference_guard BEFORE INSERT ON model_egress_previews
 FOR EACH ROW EXECUTE FUNCTION birdtie_model_live_source_reference_guard();

-- The original identity trigger compares to_jsonb minus only the explicit
-- transition columns; it already makes both new source fields immutable.
COMMIT;
