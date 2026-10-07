BEGIN;
LOCK TABLE social_intents,activity_participations,activities IN ACCESS EXCLUSIVE MODE;
ALTER TABLE social_intents ADD COLUMN converted_activity_id uuid REFERENCES activities(id);
ALTER TABLE social_intents ADD COLUMN converted_participation_id uuid REFERENCES activity_participations(id);
ALTER TABLE social_intents ADD COLUMN converted_at timestamptz;
ALTER TABLE social_intents ADD COLUMN conversion_preview_digest text;
ALTER TABLE social_intents ADD CONSTRAINT social_intent_conversion_exact CHECK(
 (converted_activity_id IS NULL AND converted_participation_id IS NULL AND converted_at IS NULL AND conversion_preview_digest IS NULL) OR
 (status='CONVERTED' AND converted_activity_id IS NOT NULL AND converted_participation_id IS NOT NULL AND converted_at IS NOT NULL AND conversion_preview_digest ~ '^[0-9a-f]{64}$'));
CREATE FUNCTION birdtie_guard_intent_activity_conversion() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p_activity uuid;p_owner uuid;p_status text;
BEGIN
 IF TG_OP='UPDATE' AND OLD.converted_activity_id IS NOT NULL THEN
  IF ROW(NEW.id,NEW.creator_account_id,NEW.status,NEW.intent_type,NEW.title,NEW.constraints,NEW.audience,NEW.modality,NEW.context_id,NEW.expires_at,NEW.created_at,NEW.converted_activity_id,NEW.converted_participation_id,NEW.converted_at,NEW.conversion_preview_digest)
   IS DISTINCT FROM ROW(OLD.id,OLD.creator_account_id,OLD.status,OLD.intent_type,OLD.title,OLD.constraints,OLD.audience,OLD.modality,OLD.context_id,OLD.expires_at,OLD.created_at,OLD.converted_activity_id,OLD.converted_participation_id,OLD.converted_at,OLD.conversion_preview_digest) THEN
   RAISE EXCEPTION 'converted intent association is immutable' USING ERRCODE='23514';
  END IF;RETURN NEW;
 END IF;
 IF NEW.converted_activity_id IS NOT NULL THEN
  IF TG_OP<>'UPDATE' OR OLD.status<>'MATCHED' OR OLD.intent_type<>'FIND_ACTIVITY' OR OLD.converted_activity_id IS NOT NULL THEN
   RAISE EXCEPTION 'conversion needs verified original matched intent' USING ERRCODE='23514';
  END IF;
  SELECT activity_id,participant_account_id,status INTO p_activity,p_owner,p_status FROM activity_participations WHERE id=NEW.converted_participation_id;
  IF p_activity IS DISTINCT FROM NEW.converted_activity_id OR p_owner IS DISTINCT FROM NEW.creator_account_id OR p_status IS DISTINCT FROM 'going' THEN
   RAISE EXCEPTION 'conversion must retain original own going participation' USING ERRCODE='23514';
  END IF;
 ELSIF NEW.status='CONVERTED' AND (TG_OP='INSERT' OR OLD.status<>'CONVERTED') THEN
  RAISE EXCEPTION 'new converted state needs current participation association' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_intent_activity_conversion BEFORE INSERT OR UPDATE ON social_intents FOR EACH ROW EXECUTE FUNCTION birdtie_guard_intent_activity_conversion();
COMMIT;
