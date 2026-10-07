BEGIN;

-- Target scope is relational and separate from free-form activity constraints.
CREATE TABLE social_intent_audience_targets (
    intent_id uuid PRIMARY KEY REFERENCES social_intents(id) ON DELETE CASCADE,
    city_id text REFERENCES cities(id) ON DELETE RESTRICT,
    community_id uuid REFERENCES communities(id) ON DELETE RESTRICT,
    CHECK ((city_id IS NOT NULL AND community_id IS NULL) OR
           (city_id IS NULL AND community_id IS NOT NULL))
);
CREATE INDEX social_intent_targets_city ON social_intent_audience_targets(city_id,intent_id)
    WHERE city_id IS NOT NULL;
CREATE INDEX social_intent_targets_community ON social_intent_audience_targets(community_id,intent_id)
    WHERE community_id IS NOT NULL;

CREATE TABLE social_intent_invitations (
    intent_id uuid NOT NULL REFERENCES social_intents(id) ON DELETE CASCADE,
    invitee_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'invited' CHECK (status IN ('invited','revoked')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(intent_id,invitee_account_id)
);
CREATE INDEX social_intent_invitations_invitee ON social_intent_invitations
    (invitee_account_id,intent_id) WHERE status='invited';

CREATE OR REPLACE FUNCTION birdtie_social_intent_audience_shape() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_intent uuid; intent_audience text;
BEGIN
    IF TG_TABLE_NAME='social_intents' THEN target_intent:=NEW.id;
    ELSIF TG_OP='DELETE' THEN target_intent:=OLD.intent_id;
    ELSE target_intent:=NEW.intent_id;
    END IF;
    SELECT audience INTO intent_audience FROM social_intents WHERE id=target_intent;
    IF intent_audience='LOCAL' AND NOT EXISTS (
        SELECT 1 FROM social_intent_audience_targets WHERE intent_id=target_intent AND city_id IS NOT NULL) THEN
        RAISE EXCEPTION 'LOCAL social intent requires a City target';
    ELSIF intent_audience='COMMUNITY' AND NOT EXISTS (
        SELECT 1 FROM social_intent_audience_targets WHERE intent_id=target_intent AND community_id IS NOT NULL) THEN
        RAISE EXCEPTION 'COMMUNITY social intent requires a Community target';
    ELSIF intent_audience NOT IN ('LOCAL','COMMUNITY') AND EXISTS (
        SELECT 1 FROM social_intent_audience_targets WHERE intent_id=target_intent) THEN
        RAISE EXCEPTION 'social intent has an unexpected audience target';
    END IF;
    -- Revoking the last invitation must remain possible for personal safety.
    IF intent_audience<>'INVITE_ONLY' AND EXISTS (
        SELECT 1 FROM social_intent_invitations WHERE intent_id=target_intent) THEN
        RAISE EXCEPTION 'social intent has unexpected invitees';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER social_intent_audience_shape
    AFTER INSERT OR UPDATE OF audience ON social_intents
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_social_intent_audience_shape();
CREATE CONSTRAINT TRIGGER social_intent_target_shape
    AFTER INSERT OR UPDATE OR DELETE ON social_intent_audience_targets
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_social_intent_audience_shape();
CREATE CONSTRAINT TRIGGER social_intent_invitee_shape
    AFTER INSERT OR UPDATE OR DELETE ON social_intent_invitations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_social_intent_audience_shape();

-- Existing drafts with these audiences need an explicit owner-chosen target.
-- Refuse migration instead of inventing City, Community or invitees.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM social_intents WHERE audience IN ('LOCAL','COMMUNITY','INVITE_ONLY')) THEN
        RAISE EXCEPTION 'existing targeted drafts require explicit audience migration';
    END IF;
END $$;

-- All public and Agent reads must reuse this server-side predicate. A
-- self-declared current City Context is a browsing scope, not residence proof.
CREATE FUNCTION birdtie_social_intent_visible_to(target_intent uuid, viewer uuid)
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

COMMIT;
