-- Accepted Agent identity and organization ownership model (ADR 2026-09-30).
-- City contexts belong to platform configuration, not an account or Agent.
CREATE TABLE IF NOT EXISTS agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_type text NOT NULL CHECK (agent_type IN ('personal', 'organization', 'system')),
    principal_account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'retired')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agents_principal_shape CHECK (
        (agent_type = 'system' AND principal_account_id IS NULL) OR
        (agent_type IN ('personal', 'organization') AND principal_account_id IS NOT NULL)
    ),
    UNIQUE (agent_type, principal_account_id)
);

CREATE TABLE IF NOT EXISTS organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    organization_type text NOT NULL CHECK (organization_type IN
        ('student_society', 'club', 'business', 'university', 'community', 'venue', 'nonprofit', 'other')),
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 160),
    profile jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION birdtie_validate_organization_account() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id = NEW.account_id AND account_type = 'organization') THEN
        RAISE EXCEPTION 'organization requires organization principal account';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS organizations_validate_account ON organizations;
CREATE TRIGGER organizations_validate_account BEFORE INSERT OR UPDATE
    ON organizations FOR EACH ROW EXECUTE FUNCTION birdtie_validate_organization_account();

CREATE TABLE IF NOT EXISTS organization_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('owner', 'admin', 'moderator', 'member')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'invited', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, user_account_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS organization_one_active_owner
    ON organization_memberships (organization_id)
    WHERE role = 'owner' AND status = 'active';
CREATE INDEX IF NOT EXISTS organization_memberships_user_active
    ON organization_memberships (user_account_id, organization_id) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS city_contexts (
    city_id text PRIMARY KEY REFERENCES cities(id) ON DELETE CASCADE,
    geographic_boundary jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    discovery_configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION birdtie_validate_agent_principal() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE principal_type text;
BEGIN
    SELECT account_type INTO principal_type FROM accounts WHERE id = NEW.principal_account_id;
    IF NEW.agent_type = 'system' AND NEW.principal_account_id IS NOT NULL THEN
        RAISE EXCEPTION 'system agent is platform-owned and has no account';
    ELSIF NEW.agent_type = 'personal' AND principal_type <> 'person' THEN
        RAISE EXCEPTION 'personal agent requires person principal';
    ELSIF NEW.agent_type = 'organization' AND principal_type <> 'organization' THEN
        RAISE EXCEPTION 'organization agent requires organization principal';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS agents_validate_principal ON agents;
CREATE TRIGGER agents_validate_principal BEFORE INSERT OR UPDATE
    ON agents FOR EACH ROW EXECUTE FUNCTION birdtie_validate_agent_principal();

CREATE OR REPLACE FUNCTION birdtie_validate_organization_membership() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE org_account uuid; person_type text;
BEGIN
    SELECT account_id INTO org_account FROM organizations WHERE id = NEW.organization_id;
    SELECT account_type INTO person_type FROM accounts WHERE id = NEW.user_account_id;
    IF org_account IS NULL OR person_type <> 'person' OR NEW.user_account_id = org_account THEN
        RAISE EXCEPTION 'organization membership requires a distinct person account';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS organization_memberships_validate_principals ON organization_memberships;
CREATE TRIGGER organization_memberships_validate_principals
    BEFORE INSERT OR UPDATE ON organization_memberships
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_organization_membership();

-- Every existing person receives exactly one Personal Agent. No City Agent is created.
INSERT INTO agents (agent_type, principal_account_id)
SELECT 'personal', id FROM accounts WHERE account_type = 'person'
ON CONFLICT (agent_type, principal_account_id) DO NOTHING;

-- The context is platform-managed and intentionally contains no owner/account field.
INSERT INTO city_contexts (city_id)
SELECT id FROM cities ON CONFLICT (city_id) DO NOTHING;
