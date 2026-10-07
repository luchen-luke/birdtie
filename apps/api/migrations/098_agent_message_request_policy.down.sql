BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM agent_message_request_policies) OR EXISTS(SELECT 1 FROM connection_request_policy_bindings) OR EXISTS(SELECT 1 FROM audit_events WHERE purpose='human_message_policy_edit') THEN RAISE EXCEPTION '098 down refuses to erase message routing preferences/history';END IF;END $$;
DROP TABLE connection_request_policy_bindings;
DROP FUNCTION birdtie_guard_message_request_binding();
DROP TABLE agent_message_request_policies;
DROP FUNCTION birdtie_guard_message_request_policy();
COMMIT;
