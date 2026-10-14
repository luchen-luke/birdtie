BEGIN;
-- Extend original 062 preview only. No new Run, budget account or authority store.
CREATE OR REPLACE FUNCTION birdtie_model_licensed_place_batch_shape(batch jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE fact jsonb; item jsonb; source jsonb;
BEGIN
 IF batch IS NULL OR jsonb_typeof(batch)<>'object'
  OR NOT batch ?& ARRAY['schema','context','facts','places','cityProof','fieldProof','queryDigest','deadlineAt']
  OR (SELECT count(*) FROM jsonb_object_keys(batch))<>8
  OR jsonb_typeof(batch->'schema') IS DISTINCT FROM 'string'
  OR jsonb_typeof(batch->'cityProof') IS DISTINCT FROM 'string'
  OR jsonb_typeof(batch->'fieldProof') IS DISTINCT FROM 'string'
  OR jsonb_typeof(batch->'queryDigest') IS DISTINCT FROM 'string'
  OR jsonb_typeof(batch->'deadlineAt') IS DISTINCT FROM 'string'
  OR batch->>'schema'<>'birdtie.native-licensed-place-batch.v1'
  OR jsonb_typeof(batch->'context')<>'object'
  OR NOT batch->'context' ?& ARRAY['schema','city','slots','places','sources']
  OR (SELECT count(*) FROM jsonb_object_keys(batch->'context'))<>5
  OR jsonb_typeof(batch->'context'->'schema') IS DISTINCT FROM 'string'
  OR jsonb_typeof(batch->'context'->'city') IS DISTINCT FROM 'object'
  OR NOT batch->'context'->'city' ?& ARRAY['id','name','countryCode']
  OR (SELECT count(*) FROM jsonb_object_keys(batch->'context'->'city'))<>3
  OR jsonb_typeof(batch->'context'->'slots') IS DISTINCT FROM 'object'
  OR NOT batch->'context'->'slots' ?& ARRAY['operation','target','category','timePreference','distancePreference','searchTerm']
  OR (SELECT count(*) FROM jsonb_object_keys(batch->'context'->'slots'))<>6
  OR jsonb_typeof(batch->'context'->'slots'->'operation') IS DISTINCT FROM 'string'
  OR jsonb_typeof(batch->'context'->'slots'->'target') IS DISTINCT FROM 'string'
  OR batch->'context'->>'schema'<>'birdtie.licensed-place-context.v1'
  OR batch->'context'->'slots'->>'operation'<>'FIND_PLACE'
  OR batch->'context'->'slots'->>'target'<>'FIND_PLACE'
  OR batch->>'cityProof' !~ '^[0-9a-f]{64}$'
  OR batch->>'fieldProof' !~ '^[0-9a-f]{64}$'
  OR batch->>'queryDigest' !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(batch->'facts')<>'array' OR jsonb_array_length(batch->'facts') NOT BETWEEN 1 AND 60
  OR jsonb_typeof(batch->'places')<>'array' OR jsonb_array_length(batch->'places') NOT BETWEEN 1 AND 10
  OR jsonb_typeof(batch->'context'->'places')<>'array'
  OR jsonb_array_length(batch->'context'->'places')<>jsonb_array_length(batch->'places')
  OR jsonb_typeof(batch->'context'->'sources')<>'array'
  OR jsonb_array_length(batch->'context'->'sources') NOT BETWEEN 1 AND 10 THEN RETURN false; END IF;
 FOR fact IN SELECT value FROM jsonb_array_elements(batch->'facts') LOOP
  IF jsonb_typeof(fact)<>'object' OR NOT fact ?& ARRAY['evidenceId','placeId','field','version','permissionId']
   OR (SELECT count(*) FROM jsonb_object_keys(fact))<>5
   OR jsonb_typeof(fact->'evidenceId') IS DISTINCT FROM 'string'
   OR jsonb_typeof(fact->'placeId') IS DISTINCT FROM 'string'
   OR jsonb_typeof(fact->'permissionId') IS DISTINCT FROM 'string'
   OR jsonb_typeof(fact->'field') IS DISTINCT FROM 'string'
   OR fact->>'evidenceId' !~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$'
   OR fact->>'placeId' !~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$'
   OR fact->>'permissionId' !~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$'
   OR fact->>'field' NOT IN('name','address','coordinates','food_seafood','food_spicy','opening_hours')
   OR jsonb_typeof(fact->'version')<>'number' OR (fact->>'version')::bigint<1 THEN RETURN false; END IF;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(batch->'places') LOOP
  IF jsonb_typeof(item)<>'object' OR NOT item ?& ARRAY['id','generation']
   OR (SELECT count(*) FROM jsonb_object_keys(item))<>2
   OR jsonb_typeof(item->'id') IS DISTINCT FROM 'string'
   OR jsonb_typeof(item->'generation') IS DISTINCT FROM 'string'
   OR item->>'id' !~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$'
   OR item->>'generation' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 END LOOP;
 FOR source IN SELECT value FROM jsonb_array_elements(batch->'context'->'sources') LOOP
  IF jsonb_typeof(source) IS DISTINCT FROM 'object'
   OR NOT source ?& ARRAY['title','url','passage']
   OR (SELECT count(*) FROM jsonb_object_keys(source))<>3
   OR jsonb_typeof(source->'title') IS DISTINCT FROM 'string'
   OR jsonb_typeof(source->'url') IS DISTINCT FROM 'string'
   OR jsonb_typeof(source->'passage') IS DISTINCT FROM 'string'
   OR source->>'passage'<>'' THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER TABLE model_egress_previews DROP CONSTRAINT IF EXISTS model_egress_preview_scope_closed;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_egress_preview_scope_closed CHECK(scope IN (
 'SELF_TASK_QUERY','SELF_TASK_QUERY_PUBLIC_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES','SELF_TASK_QUERY_LICENSED_PLACES'));
ALTER TABLE model_egress_previews DROP CONSTRAINT IF EXISTS model_live_source_export_shape;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_live_source_export_shape CHECK(
 (scope='SELF_TASK_QUERY' AND source_batch IS NULL AND source_evidence_digest IS NULL)
 OR (scope='SELF_TASK_PUBLIC_RESOLVED_SEARCH' AND billing_kind='CALL' AND purpose='PUBLIC_SOURCE_RETRIEVAL' AND source_batch IS NULL AND source_evidence_digest IS NULL)
 OR (scope IN ('SELF_TASK_QUERY_PUBLIC_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES') AND billing_kind='TOKEN' AND purpose='MODEL_CONTEXT_EGRESS'
  AND source_batch IS NOT NULL AND source_evidence_digest IS NOT NULL AND source_evidence_digest~'^[0-9a-f]{64}$' AND source_evidence_digest<>repeat('0',64)
  AND octet_length(source_batch::text)<=16384 AND birdtie_model_live_source_batch_shape(source_batch)
  AND source_batch->'evidence'->>'QueryEvidenceDigest'=current_query_digest)
 OR (scope='SELF_TASK_QUERY_LICENSED_PLACES' AND billing_kind='TOKEN' AND purpose='MODEL_CONTEXT_EGRESS'
  AND source_batch IS NOT NULL AND source_evidence_digest IS NOT NULL AND source_evidence_digest~'^[0-9a-f]{64}$' AND source_evidence_digest<>repeat('0',64)
  AND octet_length(source_batch::text)<=16384 AND birdtie_model_licensed_place_batch_shape(source_batch)
  AND source_batch->>'queryDigest'=current_query_digest)
);
CREATE OR REPLACE FUNCTION birdtie_model_live_source_reference_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_operation uuid; call_preview model_egress_previews%ROWTYPE;
 attempt model_budget_reservations%ROWTYPE; observed timestamptz; deadline timestamptz;
 expected_call_scope text; fact jsonb; field_current boolean;
BEGIN
 IF NEW.scope='SELF_TASK_QUERY_LICENSED_PLACES' THEN
  IF NEW.billing_kind<>'TOKEN' OR NEW.purpose<>'MODEL_CONTEXT_EGRESS'
   OR NOT birdtie_model_licensed_place_batch_shape(NEW.source_batch)
   OR NEW.source_batch->>'queryDigest'<>NEW.current_query_digest
   OR (NEW.source_batch->>'deadlineAt')::timestamptz<>NEW.expires_at
   OR NEW.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'invalid native licensed-place export'; END IF;
  FOR fact IN SELECT value FROM jsonb_array_elements(NEW.source_batch->'facts') LOOP
   SELECT true INTO field_current
   FROM place_field_evidence f JOIN place_source_permissions p ON p.id=f.permission_id
    JOIN place_candidates c ON c.id=f.source_candidate_id AND c.permission_id=p.id AND c.status IN('published','linked_duplicate')
    JOIN places pl ON pl.id=f.place_id AND pl.publication_status='published'
    JOIN cities ci ON ci.id=pl.city_id AND ci.publication_status='published'
   WHERE f.id=(fact->>'evidenceId')::uuid AND f.place_id=(fact->>'placeId')::uuid
    AND f.field_key=fact->>'field' AND f.version=(fact->>'version')::bigint
    AND f.permission_id=(fact->>'permissionId')::uuid AND f.permission_version=p.version
    AND p.state='CONFIRMED' AND p.revoked_at IS NULL AND p.reviewed_by IS NOT NULL
    AND p.reviewed_by<>p.declared_by AND p.reviewed_at<=clock_timestamp()
    AND p.acquisition='OPEN_LICENSE' AND p.origin_provider='OPEN_DATA_DOWNLOAD'
    AND p.license_url='https://opendatacommons.org/licenses/odbl/1-0/' AND p.license_version='ODbL-1.0'
    AND p.retention_until>=NEW.expires_at AND f.valid_to>=NEW.expires_at AND f.refresh_due>=NEW.expires_at
    AND f.valid_from<=clock_timestamp() AND f.conflict_state='NONE'
    AND f.observed_at<=clock_timestamp() AND f.verified_at<=clock_timestamp()
    AND pl.city_id=NEW.source_batch->'context'->'city'->>'id'
    AND (pl.expires_at IS NULL OR pl.expires_at>=NEW.expires_at)
    AND (ci.expires_at IS NULL OR ci.expires_at>=NEW.expires_at)
   FOR SHARE OF f,p,c,pl,ci;
   IF field_current IS DISTINCT FROM true THEN RAISE EXCEPTION 'current licensed field permission required'; END IF;
  END LOOP;
  RETURN NEW;
 END IF;
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
COMMIT;

