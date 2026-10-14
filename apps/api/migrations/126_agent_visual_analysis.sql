BEGIN;

-- Dedicated pixel purpose; original four ledgers and immutable tariff registry remain authoritative.

ALTER TABLE model_local_price_versions DROP CONSTRAINT model_live_price_shape;
ALTER TABLE model_local_price_versions ADD CONSTRAINT model_live_price_shape CHECK(
 (billing_kind='LOCAL' AND retention='NO_STATE_NO_STORAGE' AND evidence='LOCAL_SYNTHETIC'
  AND input_rate BETWEEN 1 AND 1000000 AND output_rate BETWEEN 1 AND 1000000
  AND input_ceiling BETWEEN 1 AND 1000000 AND output_ceiling BETWEEN 1 AND 4096
  AND call_micros=0 AND snapshot_source_url IS NULL AND snapshot_sha256 IS NULL
  AND snapshot_document_updated_at IS NULL AND snapshot_observed_at IS NULL AND snapshot_expires_at IS NULL)
 OR
 (billing_kind IN ('TOKEN','CALL') AND retention='UNKNOWN' AND evidence='PUBLISHED_TARIFF_SNAPSHOT'
  AND currency='CNY' AND region='APAC' AND snapshot_source_url IS NOT NULL AND snapshot_sha256 IS NOT NULL
  AND snapshot_sha256~'^[0-9a-f]{64}$' AND snapshot_sha256<>repeat('0',64)
  AND snapshot_document_updated_at IS NOT NULL AND snapshot_observed_at IS NOT NULL AND snapshot_expires_at IS NOT NULL
  AND isfinite(snapshot_document_updated_at) AND isfinite(snapshot_observed_at) AND isfinite(snapshot_expires_at)
  AND snapshot_document_updated_at<=snapshot_observed_at AND snapshot_observed_at<=created_at
  AND snapshot_expires_at=expires_at AND snapshot_expires_at>snapshot_observed_at
  AND snapshot_expires_at<=snapshot_observed_at+interval '24 hours'
  AND ((billing_kind='TOKEN' AND provider_id='tencent_tokenhub' AND model_id='hy3' AND model_version='hy3'
        AND wire_contract='tokenhub.chat-completions.v1' AND input_rate=1 AND output_rate=4
        AND input_ceiling=196608 AND output_ceiling BETWEEN 1 AND 768 AND call_micros=0
        AND snapshot_source_url='https://cloud.tencent.com/document/product/1823/130055')
    OR (billing_kind='TOKEN' AND provider_id='tencent_tokenhub' AND model_id='deepseek-v4-pro-0813' AND model_version='deepseek-v4-pro-0813'
        AND wire_contract='tokenhub.chat-completions.v1' AND input_rate=9 AND output_rate=27
        AND input_ceiling=16384 AND output_ceiling BETWEEN 1 AND 768 AND call_micros=0
        AND snapshot_source_url='https://cloud.tencent.com/document/product/1823/130055')
    OR (billing_kind='TOKEN' AND provider_id='tencent_tokenhub' AND model_id='hy-vision-2.0-instruct' AND model_version='hy-vision-2.0-instruct' AND wire_contract='tokenhub.visual.chat-completions.v1' AND input_rate=8 AND output_rate=18 AND input_ceiling=24576 AND output_ceiling=768 AND call_micros=0 AND snapshot_source_url='https://cloud.tencent.com/document/product/1823/130055')
    OR (billing_kind='CALL' AND provider_id='tencent_wsa' AND model_id='searchpro' AND model_version='searchpro'
        AND wire_contract='wsa.search-pro.v1' AND input_rate=0 AND output_rate=0 AND input_ceiling=0 AND output_ceiling=0
        AND call_micros=80000 AND snapshot_source_url='https://cloud.tencent.com/document/product/1806/121798')))
);

ALTER TABLE model_budget_reservations DROP CONSTRAINT model_live_reservation_shape;
ALTER TABLE model_budget_reservations ADD CONSTRAINT model_live_reservation_shape CHECK(
 (billing_kind='LOCAL' AND execution_status='UNAVAILABLE' AND cash_status='LOCAL_SYNTHETIC' AND usage_status='UNKNOWN'
  AND upper_input BETWEEN 1 AND 1000000 AND upper_output BETWEEN 1 AND 4096
  AND ((state='SETTLED' AND reported_input IS NOT NULL AND reported_output IS NOT NULL AND settled_cost IS NOT NULL)
    OR (state<>'SETTLED' AND reported_input IS NULL AND reported_output IS NULL AND settled_cost IS NULL)))
 OR
 (billing_kind IN ('TOKEN','CALL') AND currency='CNY' AND cash_status='UNKNOWN' AND settled_cost IS NULL
  AND upper_cost<=200000
  AND ((billing_kind='TOKEN' AND upper_input=196608 AND upper_output BETWEEN 1 AND 768 AND upper_cost=upper_input+4*upper_output)
    OR (billing_kind='TOKEN' AND upper_input=16384 AND upper_output BETWEEN 1 AND 768 AND upper_cost=9*upper_input+27*upper_output)
    OR (billing_kind='CALL' AND upper_input=0 AND upper_output=0 AND upper_cost=80000 AND usage_status='UNKNOWN'))
  AND ((state='RESERVED' AND execution_status='LIVE_RESERVED' AND usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL)
    OR (state='IN_FLIGHT' AND execution_status='LIVE_IN_FLIGHT' AND usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL)
    OR (state='UNKNOWN' AND execution_status='LIVE_ATTEMPTED'
        AND ((usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL)
          OR (billing_kind='TOKEN' AND usage_status='KNOWN' AND reported_input IS NOT NULL AND reported_output IS NOT NULL)))))
 OR (billing_kind='TOKEN' AND currency='CNY' AND cash_status='UNKNOWN' AND settled_cost IS NULL AND upper_input=24576 AND upper_output=768 AND upper_cost=210432 AND ((state='RESERVED' AND execution_status='LIVE_RESERVED' AND usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL) OR (state='IN_FLIGHT' AND execution_status='LIVE_IN_FLIGHT' AND usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL) OR (state='UNKNOWN' AND execution_status='LIVE_ATTEMPTED' AND ((usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL) OR (usage_status='KNOWN' AND reported_input IS NOT NULL AND reported_output IS NOT NULL)))))
);

ALTER TABLE model_egress_previews DROP CONSTRAINT model_live_preview_shape;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_live_preview_shape CHECK(
 (billing_kind='LOCAL' AND max_output_tokens BETWEEN 1 AND 4096 AND purpose='MODEL_CONTEXT_EGRESS'
  AND current_query_digest IS NULL AND egress_payload_digest IS NULL)
 OR
 (billing_kind IN ('TOKEN','CALL') AND current_query_digest IS NOT NULL AND egress_payload_digest IS NOT NULL
  AND current_query_digest~'^[0-9a-f]{64}$' AND egress_payload_digest~'^[0-9a-f]{64}$'
  AND ((billing_kind='TOKEN' AND max_output_tokens BETWEEN 1 AND 768 AND purpose='MODEL_CONTEXT_EGRESS')
    OR (billing_kind='CALL' AND max_output_tokens=0 AND purpose='PUBLIC_SOURCE_RETRIEVAL')))
 OR (scope='SELF_VISUAL_PIXEL_ANALYSIS' AND billing_kind='TOKEN' AND max_output_tokens=768 AND purpose='MODEL_VISUAL_PIXEL_EGRESS' AND current_query_digest~'^[0-9a-f]{64}$' AND egress_payload_digest~'^[0-9a-f]{64}$')
);

ALTER TABLE model_egress_previews DROP CONSTRAINT model_live_source_export_shape;
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
 OR (scope='SELF_VISUAL_PIXEL_ANALYSIS' AND billing_kind='TOKEN' AND purpose='MODEL_VISUAL_PIXEL_EGRESS' AND source_batch IS NULL AND source_evidence_digest IS NULL)
);

ALTER TABLE model_egress_previews DROP CONSTRAINT model_egress_preview_scope_closed;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_egress_preview_scope_closed CHECK(scope IN ('SELF_TASK_QUERY','SELF_TASK_QUERY_PUBLIC_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES','SELF_TASK_QUERY_LICENSED_PLACES','SELF_VISUAL_PIXEL_ANALYSIS'));

CREATE TABLE agent_visual_previews (
 id uuid PRIMARY KEY,
 operation_id uuid NOT NULL UNIQUE,
 owner_id uuid NOT NULL REFERENCES accounts(id),
 agent_id uuid NOT NULL REFERENCES agents(id),
 session_id uuid NOT NULL REFERENCES sessions(id),
 session_generation bigint NOT NULL CHECK(session_generation>0),
 context_id uuid NOT NULL REFERENCES contexts(id),
 root_trace_id uuid NOT NULL REFERENCES model_budget_roots(root_trace_id),
 source_token text NOT NULL CHECK(source_token~'^[0-9a-f]{64}$'),
 authority_token text NOT NULL CHECK(authority_token~'^[0-9a-f]{64}$'),
 input_digest text NOT NULL CHECK(input_digest~'^[0-9a-f]{64}$'),
 request_digest text NOT NULL CHECK(request_digest~'^[0-9a-f]{64}$'),
 payload_digest text NOT NULL CHECK(payload_digest~'^[0-9a-f]{64}$'),
 input_json jsonb NOT NULL CHECK(octet_length(input_json::text)<=8192),
 preview_json jsonb NOT NULL CHECK(octet_length(preview_json::text)<=8192),
 pixels bytea CHECK(pixels IS NULL OR octet_length(pixels) BETWEEN 1 AND 10485760),
 pixel_sha256 text NOT NULL CHECK(pixel_sha256~'^[0-9a-f]{64}$'),
 pixel_id uuid NOT NULL UNIQUE,
 version bigint NOT NULL DEFAULT 1 CHECK(version=1),
 state text NOT NULL DEFAULT 'PREVIEW' CHECK(state IN ('PREVIEW','APPROVED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at AND expires_at<=created_at+interval '2 minutes'),
 FOREIGN KEY(id) REFERENCES model_egress_previews(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE agent_visual_operations (
 operation_id uuid PRIMARY KEY REFERENCES model_budget_reservations(operation_id) DEFERRABLE INITIALLY DEFERRED,
 preview_id uuid NOT NULL UNIQUE REFERENCES agent_visual_previews(id),
 owner_id uuid NOT NULL REFERENCES accounts(id),
 status text NOT NULL DEFAULT 'UNKNOWN' CHECK(status IN ('UNKNOWN','COMPLETED','REFUSED','UNAVAILABLE')),
 report jsonb CHECK(report IS NULL OR octet_length(report::text)<=131072),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 CHECK((status='COMPLETED' AND report IS NOT NULL AND completed_at IS NOT NULL) OR (status IN ('REFUSED','UNAVAILABLE') AND report IS NULL AND completed_at IS NOT NULL) OR (status='UNKNOWN' AND report IS NULL AND completed_at IS NULL))
);
CREATE FUNCTION birdtie_agent_visual_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='agent_visual_previews' THEN
  IF to_jsonb(NEW)-ARRAY['state','pixels']::text[] <> to_jsonb(OLD)-ARRAY['state','pixels']::text[]
   OR (NEW.state<>OLD.state AND NOT(OLD.state='PREVIEW' AND NEW.state='APPROVED'))
   OR (NEW.pixels IS DISTINCT FROM OLD.pixels AND NOT(OLD.pixels IS NOT NULL AND NEW.pixels IS NULL)) THEN RAISE EXCEPTION 'exact visual preview required'; END IF;
 ELSE
  IF to_jsonb(NEW)-ARRAY['status','report','completed_at']::text[]<>to_jsonb(OLD)-ARRAY['status','report','completed_at']::text[] OR OLD.status<>'UNKNOWN' OR NEW.status='UNKNOWN' THEN RAISE EXCEPTION 'visual operation is terminal'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_visual_preview_immutable BEFORE UPDATE ON agent_visual_previews FOR EACH ROW EXECUTE FUNCTION birdtie_agent_visual_immutable();
CREATE TRIGGER agent_visual_operation_immutable BEFORE UPDATE ON agent_visual_operations FOR EACH ROW EXECUTE FUNCTION birdtie_agent_visual_immutable();
-- Preserve the original source function and its exact old scopes.
DROP TRIGGER model_live_source_reference_guard ON model_egress_previews;
CREATE TRIGGER model_live_source_reference_guard BEFORE INSERT ON model_egress_previews FOR EACH ROW WHEN (NEW.scope<>'SELF_VISUAL_PIXEL_ANALYSIS') EXECUTE FUNCTION birdtie_model_live_source_reference_guard();
CREATE FUNCTION birdtie_visual_ledger_reference() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v agent_visual_previews%ROWTYPE;
BEGIN
 SELECT * INTO v FROM agent_visual_previews WHERE id=NEW.id FOR SHARE;
 IF NOT FOUND OR NEW.owner_id<>v.owner_id OR NEW.session_id<>v.session_id OR NEW.agent_id<>v.agent_id OR NEW.root_trace_id<>v.root_trace_id OR NEW.task_id<>v.root_trace_id OR NEW.binding_id<>v.id OR NEW.source_token<>v.source_token OR NEW.authority_token<>v.authority_token OR NEW.request_digest<>v.request_digest OR NEW.egress_payload_digest<>v.payload_digest OR NEW.current_query_digest<>v.input_digest OR NEW.expires_at<>v.expires_at OR NEW.purpose<>'MODEL_VISUAL_PIXEL_EGRESS' OR NEW.price_version<>v.preview_json->'budget'->>'priceVersion' THEN RAISE EXCEPTION 'native visual pixel reference required'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_visual_reference_guard BEFORE INSERT ON model_egress_previews FOR EACH ROW WHEN (NEW.scope='SELF_VISUAL_PIXEL_ANALYSIS') EXECUTE FUNCTION birdtie_visual_ledger_reference();
CREATE FUNCTION birdtie_visual_price_purpose_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE visual boolean;
BEGIN
 SELECT wire_contract='tokenhub.visual.chat-completions.v1' INTO visual FROM model_local_price_versions WHERE version=NEW.price_version;
 IF visual IS DISTINCT FROM (NEW.scope='SELF_VISUAL_PIXEL_ANALYSIS' AND NEW.purpose='MODEL_VISUAL_PIXEL_EGRESS') THEN RAISE EXCEPTION 'visual price requires independent pixel purpose'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_visual_price_purpose_guard BEFORE INSERT ON model_egress_previews FOR EACH ROW EXECUTE FUNCTION birdtie_visual_price_purpose_guard();
COMMIT;
