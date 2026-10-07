BEGIN;

-- Reserved Business identity shape only. No Business Agent is created or
-- enabled by this migration; the existing runtime pack remains unavailable.
ALTER TABLE agents DROP CONSTRAINT agents_agent_type_check;
ALTER TABLE agents ADD CONSTRAINT agents_agent_type_check
    CHECK (agent_type IN ('personal','organization','business','system'));
ALTER TABLE agents DROP CONSTRAINT agents_principal_shape;
ALTER TABLE agents ADD CONSTRAINT agents_principal_shape CHECK (
    (agent_type='system' AND principal_account_id IS NULL) OR
    (agent_type IN ('personal','organization','business') AND principal_account_id IS NOT NULL)
);
ALTER TABLE agents ADD CONSTRAINT agents_business_not_enabled
    CHECK (agent_type<>'business' OR status IN ('suspended','retired'));

CREATE OR REPLACE FUNCTION birdtie_validate_agent_principal() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE principal_type text;
BEGIN
    SELECT account_type INTO principal_type FROM accounts WHERE id=NEW.principal_account_id;
    IF NEW.agent_type='system' AND NEW.principal_account_id IS NOT NULL THEN
        RAISE EXCEPTION 'system agent is platform-owned and has no account';
    ELSIF NEW.agent_type='personal' AND principal_type IS DISTINCT FROM 'person' THEN
        RAISE EXCEPTION 'personal agent requires person principal';
    ELSIF NEW.agent_type='organization' AND principal_type IS DISTINCT FROM 'organization' THEN
        RAISE EXCEPTION 'organization agent requires organization principal';
    ELSIF NEW.agent_type='business' AND principal_type IS DISTINCT FROM 'business' THEN
        RAISE EXCEPTION 'business agent requires distinct business principal';
    END IF;
    RETURN NEW;
END $$;

-- Composite FKs enforce the complete current identity binding, including
-- concurrent account-type/Agent-principal changes; individual IDs are not proof.
ALTER TABLE accounts ADD CONSTRAINT accounts_agent_profile_binding
    UNIQUE (id,account_type);
ALTER TABLE agents ADD CONSTRAINT agents_agent_profile_binding
    UNIQUE (id,principal_account_id,agent_type);

CREATE TABLE agent_profiles (
    agent_id uuid PRIMARY KEY,
    owner_type text NOT NULL CHECK(owner_type IN ('PERSON','ORGANIZATION','BUSINESS')),
    owner_id uuid NOT NULL,
    profile_version bigint NOT NULL DEFAULT 1 CHECK(profile_version>0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- Internal binding keys, not independent content or API fields.
    owner_account_type text GENERATED ALWAYS AS (lower(owner_type)) STORED,
    owner_agent_type text GENERATED ALWAYS AS (
        CASE owner_type WHEN 'PERSON' THEN 'personal'
                        WHEN 'ORGANIZATION' THEN 'organization'
                        WHEN 'BUSINESS' THEN 'business' END
    ) STORED,
    CONSTRAINT agent_profile_current_agent FOREIGN KEY(agent_id,owner_id,owner_agent_type)
        REFERENCES agents(id,principal_account_id,agent_type) ON DELETE CASCADE,
    CONSTRAINT agent_profile_current_owner FOREIGN KEY(owner_id,owner_account_type)
        REFERENCES accounts(id,account_type) ON DELETE CASCADE,
    CONSTRAINT agent_profile_finite_times CHECK (
        isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at
    )
);
CREATE INDEX agent_profiles_owner ON agent_profiles(owner_type,owner_id);

CREATE FUNCTION birdtie_guard_agent_profile_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.profile_version<>1 THEN
            RAISE EXCEPTION 'new AgentProfile starts at version one';
        END IF;
    ELSE
        IF NEW.agent_id IS DISTINCT FROM OLD.agent_id OR
           NEW.owner_type IS DISTINCT FROM OLD.owner_type OR
           NEW.owner_id IS DISTINCT FROM OLD.owner_id OR
           NEW.created_at IS DISTINCT FROM OLD.created_at THEN
            RAISE EXCEPTION 'AgentProfile identity and creation time are immutable';
        END IF;
        IF OLD.profile_version=9223372036854775807 OR
           NEW.profile_version<>OLD.profile_version+1 THEN
            RAISE EXCEPTION 'AgentProfile revision must advance exactly once';
        END IF;
        NEW.updated_at := GREATEST(clock_timestamp(),OLD.updated_at);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER agent_profile_revision_guard BEFORE INSERT OR UPDATE ON agent_profiles
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_profile_revision();

CREATE FUNCTION birdtie_bootstrap_agent_profile() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.agent_type IN ('personal','organization','business') THEN
        INSERT INTO agent_profiles(agent_id,owner_type,owner_id)
        VALUES(NEW.id,CASE NEW.agent_type WHEN 'personal' THEN 'PERSON'
                      WHEN 'organization' THEN 'ORGANIZATION' ELSE 'BUSINESS' END,
               NEW.principal_account_id)
        ON CONFLICT(agent_id) DO NOTHING;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER agents_bootstrap_profile AFTER INSERT ON agents
    FOR EACH ROW EXECUTE FUNCTION birdtie_bootstrap_agent_profile();

-- Metadata only: existing Agent/account IDs, UserProfile and private declarations
-- are untouched. System/CityContext/Community/Place/Venue get no profile.
INSERT INTO agent_profiles(agent_id,owner_type,owner_id)
SELECT id,CASE agent_type WHEN 'personal' THEN 'PERSON'
          WHEN 'organization' THEN 'ORGANIZATION' ELSE 'BUSINESS' END,
       principal_account_id
FROM agents WHERE agent_type IN ('personal','organization','business');

COMMIT;
