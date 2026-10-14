BEGIN;

DO $$ BEGIN IF EXISTS(SELECT 1 FROM agent_visual_previews) OR EXISTS(SELECT 1 FROM model_local_price_versions WHERE wire_contract='tokenhub.visual.chat-completions.v1') THEN RAISE EXCEPTION 'retained visual records prevent rollback; stop feature instead'; END IF; END $$;

DROP TRIGGER model_visual_reference_guard ON model_egress_previews;
DROP TRIGGER model_visual_price_purpose_guard ON model_egress_previews;
DROP FUNCTION birdtie_visual_price_purpose_guard();
DROP FUNCTION birdtie_visual_ledger_reference();
DROP TRIGGER model_live_source_reference_guard ON model_egress_previews;
CREATE TRIGGER model_live_source_reference_guard BEFORE INSERT ON model_egress_previews FOR EACH ROW EXECUTE FUNCTION birdtie_model_live_source_reference_guard();
DROP TABLE agent_visual_operations;
DROP TABLE agent_visual_previews;
DROP FUNCTION birdtie_agent_visual_immutable();

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
);

ALTER TABLE model_egress_previews DROP CONSTRAINT model_egress_preview_scope_closed;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_egress_preview_scope_closed CHECK(scope IN ('SELF_TASK_QUERY','SELF_TASK_QUERY_PUBLIC_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH','SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES','SELF_TASK_QUERY_LICENSED_PLACES'));
COMMIT;
