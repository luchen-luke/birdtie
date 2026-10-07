BEGIN;

-- The original Session table owns this monotonic authorization generation.
-- Ordinary live idle renewal does not retire an unrelated map source receipt;
-- security changes and shorter windows permanently retire earlier receipts.
ALTER TABLE sessions ADD COLUMN authorization_generation bigint NOT NULL DEFAULT 1
 CHECK(authorization_generation BETWEEN 1 AND 9223372036854775807);

CREATE FUNCTION birdtie_session_authorization_generation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.authorization_generation IS DISTINCT FROM 1::bigint THEN
   RAISE EXCEPTION 'initial native session authorization generation required' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.authorization_generation IS DISTINCT FROM OLD.authorization_generation THEN
  RAISE EXCEPTION 'session authorization generation is native managed' USING ERRCODE='23514';
 END IF;
 -- All other current and future Session fields are security-relevant by
 -- default. Only a still-live, nondecreasing idle window is exempted.
 IF (to_jsonb(NEW)-ARRAY['authorization_generation','idle_expires_at']::text[])
       IS DISTINCT FROM
    (to_jsonb(OLD)-ARRAY['authorization_generation','idle_expires_at']::text[])
    OR NEW.idle_expires_at<OLD.idle_expires_at
    OR (NEW.idle_expires_at>OLD.idle_expires_at AND OLD.idle_expires_at<=clock_timestamp()) THEN
  IF OLD.authorization_generation=9223372036854775807 THEN
   RAISE EXCEPTION 'session authorization generation exhausted' USING ERRCODE='23514';
  END IF;
  NEW.authorization_generation:=OLD.authorization_generation+1;
 END IF;
 RETURN NEW;
END $$;

CREATE TRIGGER session_authorization_generation_guard BEFORE INSERT OR UPDATE ON sessions
 FOR EACH ROW EXECUTE FUNCTION birdtie_session_authorization_generation_guard();

COMMIT;
