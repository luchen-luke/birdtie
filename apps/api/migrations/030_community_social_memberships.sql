BEGIN;

-- Preserve the legacy communities table and IDs; a missing City means a
-- community is not tied to one published CityContext.
ALTER TABLE communities ALTER COLUMN city_id DROP NOT NULL;
ALTER TABLE communities
    ADD COLUMN avatar_url text,
    ADD COLUMN join_policy text NOT NULL DEFAULT 'request',
    ADD COLUMN lifecycle_status text NOT NULL DEFAULT 'active';
ALTER TABLE communities DROP CONSTRAINT communities_visibility_check;
ALTER TABLE communities ADD CONSTRAINT communities_visibility_check
    CHECK (visibility IN ('public','private','hidden'));
ALTER TABLE communities DROP CONSTRAINT community_public_confirmation;
ALTER TABLE communities ADD CONSTRAINT community_public_confirmation
    CHECK (publication_status <> 'published' OR owner_confirmed_at IS NOT NULL);
ALTER TABLE communities ADD CONSTRAINT communities_join_policy_check
    CHECK (join_policy IN ('open','request','invite_only'));
ALTER TABLE communities ADD CONSTRAINT communities_lifecycle_status_check
    CHECK (lifecycle_status IN ('active','archived'));
ALTER TABLE communities ADD CONSTRAINT communities_avatar_url_check
    CHECK (avatar_url IS NULL OR avatar_url ~ '^https://');
ALTER TABLE communities ADD CONSTRAINT communities_place_requires_city
    CHECK (place_id IS NULL OR city_id IS NOT NULL);
CREATE INDEX communities_discovery ON communities (city_id, visibility, name, id)
    WHERE lifecycle_status='active' AND publication_status='published' AND visibility <> 'hidden';

CREATE TABLE community_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
    user_account_id uuid NOT NULL REFERENCES accounts(id),
    role text NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('active','pending','invited','left','rejected')),
    invited_by_account_id uuid REFERENCES accounts(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (community_id,user_account_id),
    CHECK (role <> 'owner' OR status = 'active')
);
CREATE INDEX community_memberships_person ON community_memberships
    (user_account_id,status,community_id);
CREATE INDEX community_memberships_requests ON community_memberships
    (community_id,created_at,id) WHERE status='pending';
CREATE INDEX community_memberships_active ON community_memberships
    (community_id,role,user_account_id) WHERE status='active';

CREATE OR REPLACE FUNCTION birdtie_community_member_person() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.user_account_id AND account_type='person') THEN
        RAISE EXCEPTION 'community member must be a person account';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER community_member_person BEFORE INSERT OR UPDATE OF user_account_id
    ON community_memberships FOR EACH ROW EXECUTE FUNCTION birdtie_community_member_person();

-- Legacy SubmitCommunity still inserts only the Community row. This trigger
-- creates its owner in the same transaction, so no live row can be ownerless.
CREATE OR REPLACE FUNCTION birdtie_community_initial_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO community_memberships (community_id,user_account_id,role,status)
    VALUES (NEW.id,NEW.owner_account_id,'owner','active');
    RETURN NEW;
END $$;
CREATE TRIGGER community_initial_owner AFTER INSERT ON communities
    FOR EACH ROW EXECUTE FUNCTION birdtie_community_initial_owner();

INSERT INTO community_memberships (community_id,user_account_id,role,status)
SELECT id,owner_account_id,'owner','active' FROM communities;

CREATE OR REPLACE FUNCTION birdtie_community_requires_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_id uuid;
BEGIN
    IF TG_TABLE_NAME='communities' THEN
        target_id := NEW.id;
    ELSIF TG_OP='DELETE' THEN
        target_id := OLD.community_id;
    ELSE
        target_id := NEW.community_id;
    END IF;
    IF EXISTS (SELECT 1 FROM communities WHERE id=target_id AND lifecycle_status='active')
       AND NOT EXISTS (SELECT 1 FROM community_memberships
                       WHERE community_id=target_id AND role='owner' AND status='active') THEN
        RAISE EXCEPTION 'active community requires an active owner';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER community_owner_required
    AFTER INSERT OR UPDATE OF lifecycle_status ON communities
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_community_requires_owner();
CREATE CONSTRAINT TRIGGER community_membership_owner_required
    AFTER INSERT OR UPDATE OR DELETE ON community_memberships
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_community_requires_owner();

COMMIT;
