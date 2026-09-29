-- Domain context and lifecycle for the deterministic Agent MVP loop.
BEGIN;
DROP TRIGGER IF EXISTS agent_tasks_validate_principal ON agent_tasks;
ALTER TABLE agent_tasks DROP CONSTRAINT IF EXISTS agent_tasks_status_check;
UPDATE agent_tasks SET status = upper(status);
ALTER TABLE agent_tasks ADD CONSTRAINT agent_tasks_status_check
    CHECK (status IN ('ACTIVE', 'COMPLETED', 'FAILED'));
ALTER TABLE agent_tasks ADD COLUMN IF NOT EXISTS principal_type text NOT NULL DEFAULT 'person'
    CHECK (principal_type IN ('person', 'organization'));
ALTER TABLE agent_tasks ADD COLUMN IF NOT EXISTS acting_user_account_id uuid REFERENCES accounts(id);
ALTER TABLE agent_tasks ADD COLUMN IF NOT EXISTS intent text NOT NULL DEFAULT 'UNSUPPORTED';
ALTER TABLE agent_tasks ADD COLUMN IF NOT EXISTS city_context_id text REFERENCES city_contexts(city_id);
ALTER TABLE agent_tasks ADD COLUMN IF NOT EXISTS filters jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE agent_tasks ADD COLUMN IF NOT EXISTS conversation jsonb NOT NULL DEFAULT '[]'::jsonb;
UPDATE agent_tasks t
SET principal_type = CASE WHEN a.account_type = 'organization' THEN 'organization' ELSE 'person' END,
    acting_user_account_id = CASE WHEN a.account_type = 'person' THEN t.owner_account_id ELSE COALESCE(
        (SELECT t.acting_user_account_id
         WHERE EXISTS (
             SELECT 1 FROM organization_memberships current_membership
             JOIN organizations current_organization ON current_organization.id = current_membership.organization_id
             JOIN accounts current_actor ON current_actor.id = current_membership.user_account_id
             WHERE current_organization.account_id = t.owner_account_id
               AND current_membership.user_account_id = t.acting_user_account_id
               AND current_membership.status = 'active' AND current_actor.account_type = 'person'
               AND current_actor.status = 'active'
         )), (
        SELECT m.user_account_id
        FROM organization_memberships m
        JOIN organizations o ON o.id = m.organization_id
        JOIN accounts u ON u.id = m.user_account_id AND u.account_type = 'person' AND u.status = 'active'
        WHERE o.account_id = t.owner_account_id AND o.status = 'active' AND m.status = 'active'
        ORDER BY (m.role = 'owner') DESC, m.created_at, m.user_account_id
        LIMIT 1
    )) END,
    city_context_id = COALESCE(t.city_context_id, t.city_id),
    intent = COALESCE(t.intent, 'UNSUPPORTED'),
    status = CASE WHEN t.conversation = '[]'::jsonb AND t.intent = 'UNSUPPORTED'
        THEN 'COMPLETED' ELSE t.status END
FROM accounts a WHERE a.id = t.owner_account_id;
ALTER TABLE agent_tasks ALTER COLUMN city_context_id SET NOT NULL;
CREATE OR REPLACE FUNCTION birdtie_validate_agent_task_principal() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_organization_id uuid;
BEGIN
    IF NEW.principal_type = 'person' AND NOT EXISTS (
        SELECT 1 FROM accounts WHERE id = NEW.owner_account_id AND account_type = 'person'
    ) THEN
        RAISE EXCEPTION 'personal task requires person principal';
    ELSIF NEW.principal_type = 'organization' AND NOT EXISTS (
        SELECT 1 FROM organizations WHERE account_id = NEW.owner_account_id AND status = 'active'
    ) THEN
        RAISE EXCEPTION 'organization task requires active organization principal';
    END IF;
    IF NEW.acting_user_account_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM accounts WHERE id = NEW.acting_user_account_id AND account_type = 'person'
    ) THEN
        RAISE EXCEPTION 'agent task actor must be a person';
    END IF;
    IF NEW.principal_type = 'organization' AND (
        TG_OP = 'INSERT' OR NEW.principal_type IS DISTINCT FROM OLD.principal_type
        OR NEW.acting_user_account_id IS DISTINCT FROM OLD.acting_user_account_id
    ) THEN
        SELECT id INTO v_organization_id FROM organizations
        WHERE account_id = NEW.owner_account_id AND status = 'active';
        IF NEW.acting_user_account_id IS NULL OR NOT EXISTS (
            SELECT 1 FROM organization_memberships
            WHERE organization_memberships.organization_id = v_organization_id
              AND user_account_id = NEW.acting_user_account_id AND status = 'active'
        ) THEN
            RAISE EXCEPTION 'organization task actor must be an active organization member';
        END IF;
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS agent_tasks_validate_principal ON agent_tasks;
CREATE TRIGGER agent_tasks_validate_principal BEFORE INSERT OR UPDATE ON agent_tasks
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_agent_task_principal();
COMMIT;
