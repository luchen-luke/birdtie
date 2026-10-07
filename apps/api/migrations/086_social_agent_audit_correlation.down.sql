BEGIN;
LOCK TABLE audit_events,admin_audit_events,business_console_audit_events,business_public_profile_audit,organization_announcement_audit,model_budget_audit,agent_run_audit IN ACCESS EXCLUSIVE MODE;
DO $$
DECLARE tab text; used boolean;
BEGIN
 IF EXISTS(SELECT 1 FROM audit_events WHERE target_resource_id IS NOT NULL) THEN
  RAISE EXCEPTION 'preserve concrete audit target lineage before downgrade';
 END IF;
 FOREACH tab IN ARRAY ARRAY['audit_events','admin_audit_events','business_console_audit_events','business_public_profile_audit','organization_announcement_audit','model_budget_audit','agent_run_audit'] LOOP
  EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I WHERE request_id IS NOT NULL)',tab) INTO used;
  IF used THEN RAISE EXCEPTION 'preserve request correlation history before downgrade'; END IF;
 END LOOP;
 FOREACH tab IN ARRAY ARRAY['audit_events','admin_audit_events','business_console_audit_events','business_public_profile_audit','organization_announcement_audit','model_budget_audit','agent_run_audit'] LOOP
  EXECUTE format('ALTER TABLE %I DROP COLUMN request_id',tab);
 END LOOP;
END $$;
ALTER TABLE audit_events DROP COLUMN target_resource_id;
COMMIT;
