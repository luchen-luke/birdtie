BEGIN;

-- Extend the original immutable 062/107 registry and ledger in place.
-- The 0813 ceiling is a local bounded two-text-message engineering contract;
-- hosted tokenizer/template parity remains an inference, not a weight proof.
-- No existing tariff, hold, state, queue or transition is rewritten.
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

-- A compatible arithmetic shape alone cannot borrow a different route price.
CREATE OR REPLACE FUNCTION birdtie_model_live_reference_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE price model_local_price_versions%ROWTYPE; preview model_egress_previews%ROWTYPE;
BEGIN
 SELECT * INTO price FROM model_local_price_versions WHERE version=NEW.price_version;
 IF NOT FOUND OR price.billing_kind IS DISTINCT FROM NEW.billing_kind THEN RAISE EXCEPTION 'model price kind mismatch'; END IF;
 IF TG_TABLE_NAME='model_egress_previews' THEN
  IF NEW.billing_kind='TOKEN' AND (NEW.max_output_tokens<1 OR NEW.max_output_tokens>price.output_ceiling) THEN RAISE EXCEPTION 'model output exceeds exact price'; END IF;
 ELSE
  SELECT * INTO preview FROM model_egress_previews WHERE id=NEW.preview_id;
  IF NOT FOUND OR preview.billing_kind IS DISTINCT FROM NEW.billing_kind THEN RAISE EXCEPTION 'model preview kind mismatch'; END IF;
  IF NEW.billing_kind IN ('TOKEN','CALL') THEN
   IF NEW.price_version IS DISTINCT FROM preview.price_version OR NEW.owner_id IS DISTINCT FROM preview.owner_id
     OR NEW.root_trace_id IS DISTINCT FROM preview.root_trace_id OR NEW.task_id IS DISTINCT FROM preview.task_id
     OR NEW.request_digest IS DISTINCT FROM preview.request_digest OR NEW.currency IS DISTINCT FROM price.currency
     OR NEW.upper_input IS DISTINCT FROM price.input_ceiling OR NEW.upper_output IS DISTINCT FROM preview.max_output_tokens
     OR (NEW.billing_kind='TOKEN' AND (NEW.upper_output<1 OR NEW.upper_output>price.output_ceiling
          OR NEW.upper_cost IS DISTINCT FROM price.input_rate*NEW.upper_input+price.output_rate*NEW.upper_output))
     OR (NEW.billing_kind='CALL' AND NEW.upper_cost IS DISTINCT FROM price.call_micros) THEN
    RAISE EXCEPTION 'exact live price preview and amount required';
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;

COMMIT;
