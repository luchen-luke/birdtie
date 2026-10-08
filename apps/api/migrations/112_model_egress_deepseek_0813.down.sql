BEGIN;

-- Never delete or reprice immutable 0813 artifacts or their UNKNOWN holds.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM model_local_price_versions WHERE billing_kind='TOKEN' AND model_id='deepseek-v4-pro-0813') THEN
  RAISE EXCEPTION '0813 immutable prices exist; downgrade refused';
 END IF;
END $$;

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
    OR (billing_kind='CALL' AND upper_input=0 AND upper_output=0 AND upper_cost=80000 AND usage_status='UNKNOWN'))
  AND ((state='RESERVED' AND execution_status='LIVE_RESERVED' AND usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL)
    OR (state='IN_FLIGHT' AND execution_status='LIVE_IN_FLIGHT' AND usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL)
    OR (state='UNKNOWN' AND execution_status='LIVE_ATTEMPTED'
        AND ((usage_status='UNKNOWN' AND reported_input IS NULL AND reported_output IS NULL)
          OR (billing_kind='TOKEN' AND usage_status='KNOWN' AND reported_input IS NOT NULL AND reported_output IS NOT NULL)))))
);

CREATE OR REPLACE FUNCTION birdtie_model_live_reference_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE price_kind text; preview_kind text;
BEGIN
 SELECT billing_kind INTO price_kind FROM model_local_price_versions WHERE version=NEW.price_version;
 IF price_kind IS DISTINCT FROM NEW.billing_kind THEN RAISE EXCEPTION 'model price kind mismatch'; END IF;
 IF TG_TABLE_NAME='model_budget_reservations' THEN
  SELECT billing_kind INTO preview_kind FROM model_egress_previews WHERE id=NEW.preview_id;
  IF preview_kind IS DISTINCT FROM NEW.billing_kind THEN RAISE EXCEPTION 'model preview kind mismatch'; END IF;
 END IF;
 RETURN NEW;
END $$;

COMMIT;
