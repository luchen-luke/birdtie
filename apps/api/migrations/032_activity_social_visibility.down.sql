BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM activities WHERE visibility IN ('organizer_members','invite_only')) THEN
        RAISE EXCEPTION '032 rollback would discard social Activity visibility';
    END IF;
END $$;
DROP TRIGGER activity_organizer_visibility_check ON activity_organizers;
DROP TRIGGER activity_social_visibility_check ON activities;
DROP FUNCTION birdtie_validate_activity_social_visibility();
CREATE OR REPLACE FUNCTION birdtie_validate_activity_open_for_participation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'cancelled' THEN RETURN NEW; END IF;
    IF TG_OP = 'UPDATE' AND OLD.status = NEW.status AND OLD.activity_id = NEW.activity_id THEN RETURN NEW; END IF;
    PERFORM 1 FROM activities a WHERE a.id=NEW.activity_id AND a.publication_status='published'
      AND a.visibility='public' AND a.cancelled_at IS NULL AND a.ends_at>now() FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'activity is not open for participation' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END $$;
DROP FUNCTION IF EXISTS birdtie_activity_visible_to(uuid,uuid);
DROP TABLE activity_invitations;
ALTER TABLE activities DROP CONSTRAINT activities_visibility;
ALTER TABLE activities ADD CONSTRAINT activities_visibility
    CHECK (visibility IN ('public','unlisted','private'));
COMMIT;
