BEGIN;
-- Rollback deliberately refuses to discard membership/lifecycle information
-- that the legacy schema cannot represent.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM communities WHERE city_id IS NULL OR visibility='hidden'
        OR lifecycle_status <> 'active' OR join_policy <> 'request' OR avatar_url IS NOT NULL)
       OR EXISTS (SELECT 1 FROM community_memberships WHERE role <> 'owner' OR status <> 'active'
           OR user_account_id <> (SELECT owner_account_id FROM communities WHERE id=community_id)) THEN
        RAISE EXCEPTION '030 rollback would discard Community social data';
    END IF;
END $$;
DROP TRIGGER community_membership_owner_required ON community_memberships;
DROP TRIGGER community_owner_required ON communities;
DROP TRIGGER community_initial_owner ON communities;
DROP TRIGGER community_member_person ON community_memberships;
DROP FUNCTION birdtie_community_requires_owner();
DROP FUNCTION birdtie_community_initial_owner();
DROP FUNCTION birdtie_community_member_person();
DROP TABLE community_memberships;
DROP INDEX communities_discovery;
ALTER TABLE communities DROP CONSTRAINT communities_place_requires_city;
ALTER TABLE communities DROP CONSTRAINT communities_avatar_url_check;
ALTER TABLE communities DROP CONSTRAINT communities_lifecycle_status_check;
ALTER TABLE communities DROP CONSTRAINT communities_join_policy_check;
ALTER TABLE communities DROP COLUMN lifecycle_status, DROP COLUMN join_policy, DROP COLUMN avatar_url;
ALTER TABLE communities ALTER COLUMN city_id SET NOT NULL;
ALTER TABLE communities DROP CONSTRAINT communities_visibility_check;
ALTER TABLE communities ADD CONSTRAINT communities_visibility_check
    CHECK (visibility IN ('public','private'));
ALTER TABLE communities DROP CONSTRAINT community_public_confirmation;
ALTER TABLE communities ADD CONSTRAINT community_public_confirmation
    CHECK (publication_status <> 'published' OR (visibility='public' AND owner_confirmed_at IS NOT NULL));
COMMIT;
