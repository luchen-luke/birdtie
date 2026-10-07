BEGIN;

CREATE TABLE activity_organizers (
    activity_id uuid PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    person_account_id uuid REFERENCES accounts(id),
    community_id uuid REFERENCES communities(id),
    organization_id uuid REFERENCES organizations(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT activity_organizer_exactly_one CHECK
        (num_nonnulls(person_account_id,community_id,organization_id)=1)
);
CREATE INDEX activity_organizers_person ON activity_organizers(person_account_id)
    WHERE person_account_id IS NOT NULL;
CREATE INDEX activity_organizers_community ON activity_organizers(community_id)
    WHERE community_id IS NOT NULL;
CREATE INDEX activity_organizers_organization ON activity_organizers(organization_id)
    WHERE organization_id IS NOT NULL;

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
CREATE TRIGGER activity_organizer_validate BEFORE INSERT OR UPDATE
    ON activity_organizers FOR EACH ROW EXECUTE FUNCTION birdtie_validate_activity_organizer();

-- Existing Organization Activities are mapped by organization_id. Older City
-- seed records without that column are mapped by organization account host.
-- A person-hosted legacy record is mapped to that Person. Unmappable rows fail
-- this migration atomically rather than becoming organizerless.
INSERT INTO activity_organizers (activity_id,person_account_id,organization_id)
SELECT a.id,
       CASE WHEN o.id IS NULL AND host.account_type='person' THEN host.id END,
       o.id
FROM activities a
JOIN accounts host ON host.id=a.host_account_id
LEFT JOIN organizations o ON o.id=a.organization_id
    OR (a.organization_id IS NULL AND o.account_id=a.host_account_id);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM activities a LEFT JOIN activity_organizers ao ON ao.activity_id=a.id
               WHERE ao.activity_id IS NULL) THEN
        RAISE EXCEPTION 'unmappable Activity: migration cannot leave organizer empty';
    END IF;
END $$;

-- Compatibility for existing Organization and City Seed writers.
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
CREATE TRIGGER activity_initial_organizer AFTER INSERT ON activities
    FOR EACH ROW EXECUTE FUNCTION birdtie_initial_activity_organizer();

CREATE OR REPLACE FUNCTION birdtie_activity_requires_organizer() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_id uuid;
BEGIN
    IF TG_TABLE_NAME='activities' THEN target_id:=NEW.id;
    ELSIF TG_OP='DELETE' THEN target_id:=OLD.activity_id;
    ELSE target_id:=NEW.activity_id;
    END IF;
    IF EXISTS (SELECT 1 FROM activities WHERE id=target_id)
       AND NOT EXISTS (SELECT 1 FROM activity_organizers WHERE activity_id=target_id) THEN
        RAISE EXCEPTION 'Activity requires exactly one organizer';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER activity_organizer_required
    AFTER INSERT ON activities DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION birdtie_activity_requires_organizer();
CREATE CONSTRAINT TRIGGER activity_organizer_row_required
    AFTER DELETE OR UPDATE ON activity_organizers DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION birdtie_activity_requires_organizer();

COMMIT;
