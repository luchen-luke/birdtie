BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM social_intents) THEN
        RAISE EXCEPTION 'cannot remove V4 social intent while user drafts exist';
    END IF;
END $$;
DROP TABLE social_intents;
DROP FUNCTION birdtie_social_intent_requires_person();
COMMIT;
