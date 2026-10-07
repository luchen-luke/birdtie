BEGIN;
ALTER TABLE activities DROP CONSTRAINT activities_visibility;
ALTER TABLE activities ADD CONSTRAINT activities_visibility CHECK
    (visibility IN ('public','unlisted','private','organizer_members','invite_only'));
CREATE TABLE activity_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    invitee_account_id uuid NOT NULL REFERENCES accounts(id),
    invited_by_account_id uuid NOT NULL REFERENCES accounts(id),
    status text NOT NULL DEFAULT 'invited' CHECK(status IN ('invited','revoked')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(activity_id,invitee_account_id)
);
CREATE INDEX activity_invitations_invitee ON activity_invitations(invitee_account_id,activity_id)
    WHERE status='invited';

CREATE FUNCTION birdtie_activity_visible_to(target_activity uuid,viewer uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id
      WHERE a.id=target_activity AND (
        a.visibility='public' OR (viewer IS NOT NULL AND (
          (a.visibility='organizer_members' AND (
            EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=ao.community_id
                   AND m.user_account_id=viewer AND m.status='active') OR
            EXISTS(SELECT 1 FROM organization_memberships m WHERE m.organization_id=ao.organization_id
                   AND m.user_account_id=viewer AND m.status='active'))) OR
          (a.visibility='invite_only' AND (
            ao.person_account_id=viewer OR
            EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=ao.community_id
                   AND m.user_account_id=viewer AND m.status='active' AND m.role IN ('owner','admin')) OR
            EXISTS(SELECT 1 FROM organization_memberships m WHERE m.organization_id=ao.organization_id
                   AND m.user_account_id=viewer AND m.status='active' AND m.role IN ('owner','admin')) OR
            EXISTS(SELECT 1 FROM activity_invitations i WHERE i.activity_id=a.id
                   AND i.invitee_account_id=viewer AND i.status='invited')))
        ))));
$$;

CREATE OR REPLACE FUNCTION birdtie_validate_activity_open_for_participation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='cancelled' THEN RETURN NEW; END IF;
    IF TG_OP='UPDATE' AND OLD.status=NEW.status AND OLD.activity_id=NEW.activity_id THEN RETURN NEW; END IF;
    PERFORM 1 FROM activities a WHERE a.id=NEW.activity_id
      AND a.publication_status='published' AND birdtie_activity_visible_to(a.id,NEW.participant_account_id)
      AND a.cancelled_at IS NULL AND a.ends_at>now() FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'activity is not open for participation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_validate_activity_social_visibility() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_id uuid;
BEGIN
    IF TG_TABLE_NAME='activities' THEN target_id:=NEW.id;
    ELSIF TG_OP='DELETE' THEN target_id:=OLD.activity_id;
    ELSE target_id:=NEW.activity_id;
    END IF;
    IF EXISTS(SELECT 1 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id
              WHERE a.id=target_id AND a.visibility='organizer_members' AND ao.person_account_id IS NOT NULL) THEN
        RAISE EXCEPTION 'Person Activity cannot use organizer_members visibility';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER activity_social_visibility_check
    AFTER INSERT OR UPDATE OF visibility ON activities
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_validate_activity_social_visibility();
CREATE CONSTRAINT TRIGGER activity_organizer_visibility_check
    AFTER INSERT OR UPDATE ON activity_organizers
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_validate_activity_social_visibility();
COMMIT;
