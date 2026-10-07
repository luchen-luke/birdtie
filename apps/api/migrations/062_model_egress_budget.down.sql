BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM model_local_price_versions) OR EXISTS(SELECT 1 FROM model_budget_accounts) OR EXISTS(SELECT 1 FROM model_budget_roots)
  OR EXISTS(SELECT 1 FROM model_budget_tasks) OR EXISTS(SELECT 1 FROM model_egress_previews) OR EXISTS(SELECT 1 FROM model_budget_reservations) OR EXISTS(SELECT 1 FROM model_budget_audit)
 THEN RAISE EXCEPTION 'model budget records must be preserved; nonempty down refused'; END IF;
END $$;
DROP TABLE model_budget_audit;
DROP TABLE model_budget_reservations;
DROP TABLE model_egress_previews;
DROP TABLE model_budget_tasks;
DROP TABLE model_budget_roots;
DROP TABLE model_budget_accounts;
DROP TABLE model_local_price_versions;
DROP FUNCTION birdtie_model_budget_identity_immutable();
COMMIT;
