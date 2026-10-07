BEGIN;
-- Correlation in the original audit systems, not a new authorization/effect log.
-- Add nullable first: historical rows remain NULL, never fabricated/backfilled.
DO $$
DECLARE tab text;
BEGIN
 FOREACH tab IN ARRAY ARRAY['audit_events','admin_audit_events','business_console_audit_events','business_public_profile_audit','organization_announcement_audit','model_budget_audit','agent_run_audit'] LOOP
  EXECUTE format('ALTER TABLE %I ADD COLUMN request_id text',tab);
  EXECUTE format('ALTER TABLE %I ALTER COLUMN request_id SET DEFAULT NULLIF(current_setting(''birdtie.audit_request_id'',true),'''')',tab);
  EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK(request_id IS NULL OR (octet_length(request_id) BETWEEN 8 AND 64 AND request_id COLLATE "C" ~ ''^[A-Za-z0-9_-]{8,64}$''))',tab,tab||'_request_id_safe');
  EXECUTE format('CREATE INDEX %I ON %I(request_id) WHERE request_id IS NOT NULL',tab||'_request_correlation',tab);
 END LOOP;
END $$;
-- Keep original resource_id semantics, including the old Agent-context target.
ALTER TABLE audit_events ADD COLUMN target_resource_id uuid;
COMMENT ON COLUMN audit_events.request_id IS 'Untrusted validated HTTP correlation; NULL means not supplied. Not identity, consent, authorization or idempotency.';
COMMENT ON COLUMN audit_events.target_resource_id IS 'Optional original concrete domain/grant UUID; no private payload; historical resource_id is unchanged.';
-- In particular, do not replace/disable 077/078 append-only row/truncate guards.
COMMIT;
