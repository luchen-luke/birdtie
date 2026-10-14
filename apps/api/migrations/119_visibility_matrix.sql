BEGIN;
LOCK TABLE accounts,user_profiles,social_intents,follows IN ACCESS EXCLUSIVE MODE;

-- Ordinary human resource audiences. They do not grant Agent fields, Memory,
-- machine processing or model egress. Existing six Intent audiences stay valid.
ALTER TABLE user_profiles DROP CONSTRAINT user_profiles_visibility_check;
ALTER TABLE user_profiles ADD CONSTRAINT user_profiles_visibility_check CHECK
    (visibility IN ('private','public','followers','connections','close'));
ALTER TABLE social_intents DROP CONSTRAINT social_intents_audience_check;
ALTER TABLE social_intents ADD CONSTRAINT social_intents_audience_check CHECK
    (audience IN ('PRIVATE','PUBLIC','FOLLOWERS','CONNECTIONS','CLOSE',
                 'FRIENDS','COMMUNITY','LOCAL','INVITE_ONLY'));

CREATE FUNCTION birdtie_guard_profile_relationship_visibility() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE owner_kind text;
BEGIN
    IF NEW.visibility IN ('followers','connections','close') THEN
        -- account_type is part of the original 053 unique identity key. Keep
        -- that current key until this Profile write commits; a concurrent
        -- type change must wait and recheck its original reciprocal guard.
        SELECT account_type INTO owner_kind FROM accounts WHERE id=NEW.account_id FOR KEY SHARE;
        IF owner_kind IS DISTINCT FROM 'person' THEN
            RAISE EXCEPTION 'relationship Profile audience requires Person owner' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER user_profiles_relationship_visibility_guard BEFORE INSERT OR UPDATE OF account_id,visibility
    ON user_profiles FOR EACH ROW EXECUTE FUNCTION birdtie_guard_profile_relationship_visibility();
CREATE FUNCTION birdtie_guard_profile_relationship_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.account_type<>'person' AND EXISTS(SELECT 1 FROM user_profiles
       WHERE account_id=NEW.id AND visibility IN('followers','connections','close')) THEN
        RAISE EXCEPTION 'relationship Profile audience requires Person owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER accounts_profile_relationship_owner_guard BEFORE UPDATE OF account_type
    ON accounts FOR EACH ROW EXECUTE FUNCTION birdtie_guard_profile_relationship_owner();

CREATE FUNCTION birdtie_profile_visible_to(target uuid, viewer uuid)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM user_profiles p
        JOIN accounts owner ON owner.id=p.account_id AND owner.status='active'
        WHERE p.account_id=target
          AND (viewer IS NULL OR EXISTS(SELECT 1 FROM accounts reader WHERE reader.id=viewer AND reader.status='active'))
          AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND
            ((b.blocker_account_id=target AND b.blocked_account_id=viewer) OR
             (b.blocker_account_id=viewer AND b.blocked_account_id=target)))
          AND (viewer=target OR p.visibility='public' OR EXISTS(
            SELECT 1 FROM consent_grants g WHERE g.owner_account_id=target
              AND g.recipient_account_id=viewer AND g.resource_type='profile'
              AND g.resource_id=target::text AND g.purpose='profile_view'
              AND g.actions @> ARRAY['read']::text[] AND g.revoked_at IS NULL
              AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp()))
            OR (owner.account_type='person' AND CASE p.visibility
              WHEN 'followers' THEN EXISTS(SELECT 1 FROM follows f JOIN accounts reader ON reader.id=f.follower_account_id
                  AND reader.account_type='person' AND reader.status='active'
                  WHERE f.person_account_id=target AND f.follower_account_id=viewer)
              WHEN 'connections' THEN birdtie_connection_visible(target,viewer)
              WHEN 'close' THEN birdtie_close_connection_visible(target,viewer)
              ELSE false END)));
$$;

CREATE OR REPLACE FUNCTION birdtie_social_intent_visible_to(target_intent uuid, viewer uuid)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(
        SELECT 1 FROM social_intents i
        JOIN accounts creator ON creator.id=i.creator_account_id AND creator.account_type='person' AND creator.status='active'
        WHERE i.id=target_intent AND i.status='ACTIVE' AND i.expires_at>clock_timestamp()
          AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND
              ((b.blocker_account_id=i.creator_account_id AND b.blocked_account_id=viewer) OR
               (b.blocker_account_id=viewer AND b.blocked_account_id=i.creator_account_id)))
          AND CASE i.audience
            WHEN 'PUBLIC' THEN EXISTS(SELECT 1 FROM user_profiles p
                WHERE p.account_id=i.creator_account_id AND p.visibility='public')
            WHEN 'FOLLOWERS' THEN viewer IS NOT NULL AND EXISTS(SELECT 1 FROM follows f
                WHERE f.person_account_id=i.creator_account_id AND f.follower_account_id=viewer)
            WHEN 'FRIENDS' THEN birdtie_connection_visible(i.creator_account_id,viewer)
            WHEN 'CONNECTIONS' THEN birdtie_connection_visible(i.creator_account_id,viewer)
            WHEN 'CLOSE' THEN birdtie_close_connection_visible(i.creator_account_id,viewer)
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

-- Existing grant/public readers can follow a currently visible Person. A new
-- Follow cannot bootstrap its own FOLLOWERS access: the BEFORE trigger sees
-- only the pre-existing graph. Follow never creates accepted Tie or Close.
CREATE OR REPLACE FUNCTION birdtie_validate_follow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.follower_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Follow requires active Person';
    END IF;
    IF NEW.person_account_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=NEW.person_account_id
            AND a.account_type='person' AND a.status='active') OR
           NOT birdtie_profile_visible_to(NEW.person_account_id,NEW.follower_account_id) THEN
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
COMMIT;
