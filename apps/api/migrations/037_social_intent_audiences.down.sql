BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM social_intent_audience_targets) OR
       EXISTS (SELECT 1 FROM social_intent_invitations) THEN
        RAISE EXCEPTION 'cannot remove social intent audience data';
    END IF;
END $$;
DROP TRIGGER social_intent_invitee_shape ON social_intent_invitations;
DROP FUNCTION birdtie_social_intent_visible_to(uuid,uuid);
DROP TRIGGER social_intent_target_shape ON social_intent_audience_targets;
DROP TRIGGER social_intent_audience_shape ON social_intents;
DROP FUNCTION birdtie_social_intent_audience_shape();
DROP TABLE social_intent_invitations;
DROP TABLE social_intent_audience_targets;
COMMIT;
