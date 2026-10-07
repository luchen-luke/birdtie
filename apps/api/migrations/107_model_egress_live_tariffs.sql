BEGIN;

-- Extend the original 062 ledger in place. No second account, counter, approval
-- or task store. Existing rows retain LOCAL defaults and their old semantics.
ALTER TABLE model_local_price_versions
 ADD COLUMN billing_kind text NOT NULL DEFAULT 'LOCAL' CHECK(billing_kind IN ('LOCAL','TOKEN','CALL')),
 ADD COLUMN request_ceiling bigint NOT NULL DEFAULT 1 CHECK(request_ceiling=1),
 ADD COLUMN call_micros bigint NOT NULL DEFAULT 0 CHECK(call_micros IN (0,80000)),
 ADD COLUMN snapshot_source_url text,
 ADD COLUMN snapshot_sha256 text,
 ADD COLUMN snapshot_document_updated_at timestamptz,
 ADD COLUMN snapshot_observed_at timestamptz,
 ADD COLUMN snapshot_expires_at timestamptz;
ALTER TABLE model_local_price_versions
 DROP CONSTRAINT model_local_price_versions_input_rate_check,
 DROP CONSTRAINT model_local_price_versions_output_rate_check,
 DROP CONSTRAINT model_local_price_versions_input_ceiling_check,
 DROP CONSTRAINT model_local_price_versions_output_ceiling_check,
 DROP CONSTRAINT model_local_price_versions_evidence_check,
 DROP CONSTRAINT model_local_price_versions_retention_check;
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

ALTER TABLE model_egress_previews
 ADD COLUMN billing_kind text NOT NULL DEFAULT 'LOCAL' CHECK(billing_kind IN ('LOCAL','TOKEN','CALL')),
 ADD COLUMN current_query_digest text,
 ADD COLUMN egress_payload_digest text;
ALTER TABLE model_egress_previews
 DROP CONSTRAINT model_egress_previews_max_output_tokens_check,
 DROP CONSTRAINT model_egress_previews_purpose_check;
ALTER TABLE model_egress_previews ADD CONSTRAINT model_live_preview_shape CHECK(
 (billing_kind='LOCAL' AND max_output_tokens BETWEEN 1 AND 4096 AND purpose='MODEL_CONTEXT_EGRESS'
  AND current_query_digest IS NULL AND egress_payload_digest IS NULL)
 OR
 (billing_kind IN ('TOKEN','CALL') AND current_query_digest IS NOT NULL AND egress_payload_digest IS NOT NULL
  AND current_query_digest~'^[0-9a-f]{64}$' AND egress_payload_digest~'^[0-9a-f]{64}$'
  AND ((billing_kind='TOKEN' AND max_output_tokens BETWEEN 1 AND 768 AND purpose='MODEL_CONTEXT_EGRESS')
    OR (billing_kind='CALL' AND max_output_tokens=0 AND purpose='PUBLIC_SOURCE_RETRIEVAL')))
);

ALTER TABLE model_budget_reservations
 ADD COLUMN billing_kind text NOT NULL DEFAULT 'LOCAL' CHECK(billing_kind IN ('LOCAL','TOKEN','CALL')),
 ADD COLUMN usage_status text NOT NULL DEFAULT 'UNKNOWN' CHECK(usage_status IN ('UNKNOWN','KNOWN')),
 ADD COLUMN cash_status text NOT NULL DEFAULT 'LOCAL_SYNTHETIC' CHECK(cash_status IN ('LOCAL_SYNTHETIC','UNKNOWN'));
ALTER TABLE model_budget_reservations
 DROP CONSTRAINT model_budget_reservations_upper_input_check,
 DROP CONSTRAINT model_budget_reservations_upper_output_check,
 DROP CONSTRAINT model_budget_reservations_execution_status_check,
 DROP CONSTRAINT model_budget_reservations_check3;
-- The original reported_input/reported_output/settled_cost range checks remain.
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

-- Check typed references on insert without loosening the old transition guard.
CREATE FUNCTION birdtie_model_live_reference_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER model_live_preview_reference_guard BEFORE INSERT ON model_egress_previews FOR EACH ROW EXECUTE FUNCTION birdtie_model_live_reference_guard();
CREATE TRIGGER model_live_reservation_reference_guard BEFORE INSERT ON model_budget_reservations FOR EACH ROW EXECUTE FUNCTION birdtie_model_live_reference_guard();

CREATE OR REPLACE FUNCTION birdtie_model_budget_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='model_egress_previews' THEN
  IF to_jsonb(NEW)-ARRAY['status','revision','approved_at','revoked_at']::text[]<>to_jsonb(OLD)-ARRAY['status','revision','approved_at','revoked_at']::text[]
    OR NEW.revision<>OLD.revision+1 OR NOT ((OLD.status='DRAFT' AND NEW.status IN ('APPROVED','REVOKED')) OR (OLD.status='APPROVED' AND NEW.status='REVOKED')) THEN RAISE EXCEPTION 'exact model preview transition required'; END IF;
 ELSIF TG_TABLE_NAME='model_budget_reservations' THEN
  IF OLD.billing_kind='LOCAL' THEN
   IF to_jsonb(NEW)-ARRAY['state','reported_input','reported_output','settled_cost']::text[]<>to_jsonb(OLD)-ARRAY['state','reported_input','reported_output','settled_cost']::text[]
     OR NOT ((OLD.state='RESERVED' AND NEW.state IN ('IN_FLIGHT','CANCELLED_BEFORE_SEND')) OR (OLD.state='IN_FLIGHT' AND NEW.state IN ('SETTLED','UNKNOWN')) OR (OLD.state='UNKNOWN' AND NEW.state='SETTLED')) THEN RAISE EXCEPTION 'exact budget settlement transition required'; END IF;
  ELSE
   IF to_jsonb(NEW)-ARRAY['state','reported_input','reported_output','settled_cost','usage_status','cash_status','execution_status']::text[]<>to_jsonb(OLD)-ARRAY['state','reported_input','reported_output','settled_cost','usage_status','cash_status','execution_status']::text[]
     OR NEW.cash_status<>'UNKNOWN' OR NEW.settled_cost IS NOT NULL
     OR NOT ((OLD.state='RESERVED' AND NEW.state='IN_FLIGHT' AND OLD.execution_status='LIVE_RESERVED' AND NEW.execution_status='LIVE_IN_FLIGHT')
       OR (OLD.state='IN_FLIGHT' AND NEW.state='UNKNOWN' AND OLD.execution_status='LIVE_IN_FLIGHT' AND NEW.execution_status='LIVE_ATTEMPTED')
       OR (OLD.state='UNKNOWN' AND NEW.state='UNKNOWN' AND OLD.execution_status='LIVE_ATTEMPTED' AND NEW.execution_status='LIVE_ATTEMPTED'
           AND OLD.usage_status='UNKNOWN' AND OLD.reported_input IS NULL AND OLD.reported_output IS NULL AND NEW.usage_status='KNOWN' AND NEW.reported_input IS NOT NULL AND NEW.reported_output IS NOT NULL)) THEN RAISE EXCEPTION 'exact live accounting transition required'; END IF;
  END IF;
 ELSE
  IF to_jsonb(NEW)-ARRAY['used_requests','allocated_input','allocated_output','allocated_cost']::text[]<>to_jsonb(OLD)-ARRAY['used_requests','allocated_input','allocated_output','allocated_cost']::text[]
    OR NEW.used_requests<OLD.used_requests OR NEW.used_requests>OLD.used_requests+1 THEN RAISE EXCEPTION 'budget identity or attempt counter immutable'; END IF;
 END IF;
 RETURN NEW;
END $$;
COMMIT;
