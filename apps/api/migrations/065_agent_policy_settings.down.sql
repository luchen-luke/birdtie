BEGIN;
DO $$BEGIN
 IF EXISTS(SELECT 1 FROM agent_policy_settings) THEN RAISE EXCEPTION '065 down refuses to erase human policy settings';END IF;
END $$;
DROP TRIGGER agent_policy_settings_guard ON agent_policy_settings;
DROP TABLE agent_policy_settings;
DROP FUNCTION birdtie_agent_policy_settings_guard();
DROP FUNCTION birdtie_agent_policy_settings_valid(text,jsonb);
COMMIT;
