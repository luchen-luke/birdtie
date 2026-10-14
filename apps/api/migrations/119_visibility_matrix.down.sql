BEGIN;
LOCK TABLE accounts,user_profiles,social_intents,follows IN ACCESS EXCLUSIVE MODE;
-- Never coerce real audience selections into a broader or narrower audience.
-- Close118 has its own guarded downgrade; its independent data stays intact.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM user_profiles WHERE visibility IN('followers','connections','close')) OR
       EXISTS(SELECT 1 FROM social_intents WHERE audience IN('FOLLOWERS','CONNECTIONS','CLOSE')) THEN
        RAISE EXCEPTION '119 downgrade requires explicit new audience retirement' USING ERRCODE='23514';
    END IF;
END $$;
CREATE OR REPLACE FUNCTION birdtie_social_intent_visible_to(target_intent uuid, viewer uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(
        SELECT 1 FROM social_intents i
        JOIN accounts creator ON creator.id=i.creator_account_id AND creator.status='active'
        WHERE i.id=target_intent AND i.status='ACTIVE' AND i.expires_at>now()
          AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND
              ((b.blocker_account_id=i.creator_account_id AND b.blocked_account_id=viewer) OR
               (b.blocker_account_id=viewer AND b.blocked_account_id=i.creator_account_id)))
          AND CASE i.audience
            WHEN 'PUBLIC' THEN EXISTS(SELECT 1 FROM user_profiles p
                WHERE p.account_id=i.creator_account_id AND p.visibility='public')
            WHEN 'FRIENDS' THEN viewer IS NOT NULL AND EXISTS(
                SELECT 1 FROM person_ties t WHERE t.status='active'
                AND t.person_a_account_id=LEAST(i.creator_account_id,viewer)
                AND t.person_b_account_id=GREATEST(i.creator_account_id,viewer))
            WHEN 'COMMUNITY' THEN viewer IS NOT NULL AND EXISTS(
                SELECT 1 FROM social_intent_audience_targets target
                JOIN communities c ON c.id=target.community_id AND c.lifecycle_status='active'
                JOIN community_memberships owner_member ON owner_member.community_id=c.id
                    AND owner_member.user_account_id=i.creator_account_id AND owner_member.status='active'
                JOIN community_memberships reader_member ON reader_member.community_id=c.id
                    AND reader_member.user_account_id=viewer AND reader_member.status='active'
                WHERE target.intent_id=i.id)
            WHEN 'LOCAL' THEN viewer IS NOT NULL AND EXISTS(
                SELECT 1 FROM social_intent_audience_targets target
                JOIN cities city ON city.id=target.city_id AND city.publication_status='published'
                JOIN contexts context ON context.city_id=city.id AND context.context_type='CITY'
                JOIN person_contexts pc ON pc.context_id=context.id
                    AND pc.person_account_id=viewer AND pc.relation='current'
                JOIN user_profiles p ON p.account_id=i.creator_account_id AND p.visibility='public'
                WHERE target.intent_id=i.id)
            WHEN 'INVITE_ONLY' THEN viewer IS NOT NULL AND EXISTS(
                SELECT 1 FROM social_intent_invitations invitation
                WHERE invitation.intent_id=i.id AND invitation.invitee_account_id=viewer
                  AND invitation.status='invited')
            ELSE false END
          AND (viewer IS NULL OR EXISTS(SELECT 1 FROM accounts reader
                WHERE reader.id=viewer AND reader.account_type='person' AND reader.status='active'))
    );
$$;

CREATE OR REPLACE FUNCTION birdtie_validate_follow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.follower_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Follow requires active Person';
    END IF;
    IF NEW.person_account_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM accounts a JOIN user_profiles p ON p.account_id=a.id
            WHERE a.id=NEW.person_account_id AND a.account_type='person'
            AND a.status='active' AND p.visibility='public') OR
           EXISTS (SELECT 1 FROM account_blocks b WHERE
            (b.blocker_account_id=NEW.follower_account_id AND b.blocked_account_id=NEW.person_account_id) OR
            (b.blocker_account_id=NEW.person_account_id AND b.blocked_account_id=NEW.follower_account_id)) THEN
            RAISE EXCEPTION 'Person Follow target unavailable';
        END IF;
    ELSIF NEW.organization_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM organizations o JOIN accounts a ON a.id=o.account_id
            WHERE o.id=NEW.organization_id AND o.status='active' AND a.status='active') THEN
            RAISE EXCEPTION 'Organization Follow target unavailable';
        END IF;
    ELSIF NEW.community_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM communities c WHERE c.id=NEW.community_id
            AND c.lifecycle_status='active' AND c.publication_status='published'
            AND c.visibility='public') THEN
            RAISE EXCEPTION 'Community Follow target unavailable';
        END IF;
    ELSIF NEW.business_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
            WHERE b.id=NEW.business_id AND b.status='active' AND b.claim_status='verified'
            AND a.status='active') THEN
            RAISE EXCEPTION 'Business Follow target unavailable';
        END IF;
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER accounts_profile_relationship_owner_guard ON accounts;
DROP FUNCTION birdtie_guard_profile_relationship_owner();
DROP TRIGGER user_profiles_relationship_visibility_guard ON user_profiles;
DROP FUNCTION birdtie_guard_profile_relationship_visibility();
DROP FUNCTION birdtie_profile_visible_to(uuid,uuid);
ALTER TABLE user_profiles DROP CONSTRAINT user_profiles_visibility_check;
ALTER TABLE user_profiles ADD CONSTRAINT user_profiles_visibility_check CHECK(visibility IN('private','public'));
ALTER TABLE social_intents DROP CONSTRAINT social_intents_audience_check;
ALTER TABLE social_intents ADD CONSTRAINT social_intents_audience_check CHECK
    (audience IN('PRIVATE','FRIENDS','COMMUNITY','LOCAL','PUBLIC','INVITE_ONLY'));
COMMIT;
