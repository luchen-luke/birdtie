BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM businesses) OR
       EXISTS(SELECT 1 FROM activity_organizers WHERE business_id IS NOT NULL) OR
       EXISTS(SELECT 1 FROM accounts WHERE account_type='business') THEN
        RAISE EXCEPTION 'cannot remove Business principals or relationships while sourced data exists';
    END IF;
END $$;

DROP INDEX activity_organizers_business;
ALTER TABLE activity_organizers DROP CONSTRAINT activity_organizer_exactly_one;
ALTER TABLE activity_organizers DROP COLUMN business_id;
ALTER TABLE activity_organizers ADD CONSTRAINT activity_organizer_exactly_one
    CHECK(num_nonnulls(person_account_id,community_id,organization_id)=1);

CREATE OR REPLACE FUNCTION birdtie_validate_activity_organizer() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE activity_organization uuid; activity_host uuid;
BEGIN
    SELECT organization_id,host_account_id INTO activity_organization,activity_host
    FROM activities WHERE id=NEW.activity_id;
    IF NEW.person_account_id IS NOT NULL AND
       (NEW.person_account_id <> activity_host OR NOT EXISTS
           (SELECT 1 FROM accounts WHERE id=NEW.person_account_id AND account_type='person')) THEN
        RAISE EXCEPTION 'person organizer must be the person host';
    END IF;
    IF NEW.community_id IS NOT NULL AND activity_organization IS NOT NULL THEN
        RAISE EXCEPTION 'community activity cannot have organization_id';
    END IF;
    IF NEW.organization_id IS DISTINCT FROM activity_organization AND NOT
       (activity_organization IS NULL AND NEW.organization_id IS NOT NULL AND
        EXISTS (SELECT 1 FROM organizations WHERE id=NEW.organization_id AND account_id=activity_host)) THEN
        RAISE EXCEPTION 'organization organizer must match activity organization_id';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_initial_activity_organizer() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE mapped_organization uuid;
BEGIN
    mapped_organization := NEW.organization_id;
    IF mapped_organization IS NULL THEN
        SELECT id INTO mapped_organization FROM organizations WHERE account_id=NEW.host_account_id;
    END IF;
    IF mapped_organization IS NOT NULL THEN
        INSERT INTO activity_organizers(activity_id,organization_id)
        VALUES (NEW.id,mapped_organization);
    ELSE
        INSERT INTO activity_organizers(activity_id,person_account_id)
        VALUES (NEW.id,NEW.host_account_id);
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_validate_activity_organization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE organization_account uuid; creator_type text;
BEGIN
    IF NEW.organization_id IS NOT NULL THEN
        SELECT account_id INTO organization_account FROM organizations WHERE id = NEW.organization_id;
        IF organization_account IS NULL OR NEW.host_account_id IS DISTINCT FROM organization_account THEN
            RAISE EXCEPTION 'organization activity requires its organization principal as host';
        END IF;
    END IF;
    IF NEW.created_by_account_id IS NOT NULL THEN
        SELECT account_type INTO creator_type FROM accounts WHERE id = NEW.created_by_account_id;
        IF creator_type IS DISTINCT FROM 'person' THEN
            RAISE EXCEPTION 'activity creator must be a person account';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_activity_visible_to(target_activity uuid,viewer uuid) RETURNS boolean
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

DROP TABLE business_venue_relations;
DROP FUNCTION birdtie_verified_business_venue(uuid,uuid);
DROP TABLE business_memberships;
DROP TABLE businesses;
DROP FUNCTION birdtie_validate_business_member();
DROP FUNCTION birdtie_validate_business_principal();
ALTER TABLE accounts DROP CONSTRAINT accounts_account_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_account_type_check
    CHECK(account_type IN ('person','organization'));
COMMIT;
