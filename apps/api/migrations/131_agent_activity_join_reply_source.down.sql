-- A used reply-source approval, effect or audit is never downgraded.
BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_action_approvals WHERE action_kind='ACTIVITY_JOIN' AND binding_canonical::jsonb ? 'source_contract')
 OR EXISTS(SELECT 1 FROM agent_activity_join_effects w JOIN agent_action_approvals p ON p.id=w.approval_id WHERE p.binding_canonical::jsonb ? 'source_contract')
 OR EXISTS(SELECT 1 FROM audit_events WHERE purpose='OWN_ACTIVITY_JOIN_REPLY_ACTION') THEN
 RAISE EXCEPTION 'reply-source JOIN history prevents downgrade' USING ERRCODE='55000';END IF;
END $$;
DROP FUNCTION birdtie_activity_join_approval_check(agent_action_approvals,agent_action_approvals,text);
ALTER FUNCTION birdtie_activity_join_approval_check_v130(agent_action_approvals,agent_action_approvals,text) RENAME TO birdtie_activity_join_approval_check;
DROP FUNCTION birdtie_activity_join_base_current(uuid);
ALTER FUNCTION birdtie_activity_join_base_current_v130(uuid) RENAME TO birdtie_activity_join_base_current;
DROP FUNCTION birdtie_activity_join_target_tail(uuid,uuid);
ALTER FUNCTION birdtie_activity_join_target_tail_v130(uuid,uuid) RENAME TO birdtie_activity_join_target_tail;
DROP FUNCTION birdtie_activity_join_reply_source_current(uuid);
COMMIT;
