BEGIN;

-- No profile/revision/identity data is silently erased, including bootstrap
-- metadata. Empty down/reapply is only verified in a disposable local database.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM agent_profiles) OR EXISTS(SELECT 1 FROM agents WHERE agent_type='business') THEN
        RAISE EXCEPTION '053 down refuses to erase AgentProfile or reserved Business Agent data';
    END IF;
END $$;

DROP TRIGGER agents_bootstrap_profile ON agents;
DROP FUNCTION birdtie_bootstrap_agent_profile();
DROP TABLE agent_profiles;
DROP FUNCTION birdtie_guard_agent_profile_revision();
ALTER TABLE agents DROP CONSTRAINT agents_agent_profile_binding;
ALTER TABLE accounts DROP CONSTRAINT accounts_agent_profile_binding;
ALTER TABLE agents DROP CONSTRAINT agents_business_not_enabled;
ALTER TABLE agents DROP CONSTRAINT agents_principal_shape;
ALTER TABLE agents DROP CONSTRAINT agents_agent_type_check;
ALTER TABLE agents ADD CONSTRAINT agents_agent_type_check
    CHECK(agent_type IN ('personal','organization','system'));
ALTER TABLE agents ADD CONSTRAINT agents_principal_shape CHECK (
    (agent_type='system' AND principal_account_id IS NULL) OR
    (agent_type IN ('personal','organization') AND principal_account_id IS NOT NULL)
);

CREATE OR REPLACE FUNCTION birdtie_validate_agent_principal() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE principal_type text;
BEGIN
    SELECT account_type INTO principal_type FROM accounts WHERE id=NEW.principal_account_id;
    IF NEW.agent_type='system' AND NEW.principal_account_id IS NOT NULL THEN
        RAISE EXCEPTION 'system agent is platform-owned and has no account';
    ELSIF NEW.agent_type='personal' AND principal_type<>'person' THEN
        RAISE EXCEPTION 'personal agent requires person principal';
    ELSIF NEW.agent_type='organization' AND principal_type<>'organization' THEN
        RAISE EXCEPTION 'organization agent requires organization principal';
    END IF;
    RETURN NEW;
END $$;

COMMIT;
