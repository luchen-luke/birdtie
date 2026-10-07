BEGIN;
LOCK TABLE social_intents,activity_participations,activities IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM social_intents WHERE converted_activity_id IS NOT NULL OR converted_participation_id IS NOT NULL OR converted_at IS NOT NULL OR conversion_preview_digest IS NOT NULL) THEN
 RAISE EXCEPTION '085 down refuses used original intent associations';END IF;END $$;
DROP TRIGGER guard_intent_activity_conversion ON social_intents;
DROP FUNCTION birdtie_guard_intent_activity_conversion();
ALTER TABLE social_intents DROP CONSTRAINT social_intent_conversion_exact;
ALTER TABLE social_intents DROP COLUMN converted_activity_id,DROP COLUMN converted_participation_id,DROP COLUMN converted_at,DROP COLUMN conversion_preview_digest;
COMMIT;
