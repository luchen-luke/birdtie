-- Prevent a later RSVP implementation or direct database client from joining
-- an unpublished, cancelled, private, or completed Activity.
BEGIN;
CREATE OR REPLACE FUNCTION birdtie_validate_activity_open_for_participation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'cancelled' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status = NEW.status AND OLD.activity_id = NEW.activity_id THEN
            RETURN NEW;
        END IF;
    END IF;
    PERFORM 1 FROM activities a
    WHERE a.id = NEW.activity_id
      AND a.publication_status = 'published'
      AND a.visibility = 'public'
      AND a.cancelled_at IS NULL
      AND a.ends_at > now()
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'activity is not open for participation'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS activity_participation_open_guard ON activity_participations;
CREATE TRIGGER activity_participation_open_guard
    BEFORE INSERT OR UPDATE OF status, activity_id ON activity_participations
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_activity_open_for_participation();
COMMIT;
