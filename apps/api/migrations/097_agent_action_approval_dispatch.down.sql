BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_action_approvals) OR EXISTS(SELECT 1 FROM agent_action_dispatches) OR EXISTS(SELECT 1 FROM agent_sandbox_writes) OR EXISTS(SELECT 1 FROM consent_grants WHERE purpose='OWN_SANDBOX_ACTION') OR EXISTS(SELECT 1 FROM audit_events WHERE purpose='OWN_SANDBOX_ACTION') THEN RAISE EXCEPTION 'used sandbox action history prevents down' USING ERRCODE='55000';END IF;
END $$;
DROP TRIGGER agent_action_grant_guard ON consent_grants;
DROP TABLE agent_sandbox_writes;
DROP TABLE agent_action_dispatches;
DROP TABLE agent_action_approvals;
DROP FUNCTION birdtie_sandbox_action_atomic_guard();
DROP FUNCTION birdtie_sandbox_write_guard();
DROP FUNCTION birdtie_sandbox_action_dispatch_guard();
DROP FUNCTION birdtie_sandbox_action_grant_guard();
DROP FUNCTION birdtie_sandbox_action_approval_guard();
DROP FUNCTION birdtie_sandbox_action_current(uuid);
DROP FUNCTION birdtie_sandbox_action_authority(uuid,uuid,uuid);
COMMIT;
