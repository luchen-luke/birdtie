BEGIN;

ALTER TABLE accounts DROP CONSTRAINT accounts_account_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_account_type_check
    CHECK (account_type IN ('person','organization','business'));

-- New Business principals are never inferred from legacy Organization rows.
CREATE TABLE businesses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL UNIQUE REFERENCES accounts(id),
    name text NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK(length(description)<=3000),
    claim_status text NOT NULL DEFAULT 'pending'
        CHECK(claim_status IN ('pending','verified','rejected','revoked')),
    status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','hidden','closed')),
    claim_source_url text,
    claim_reviewed_by uuid REFERENCES accounts(id),
    claim_reviewed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT business_claim_evidence CHECK
        (claim_status <> 'verified' OR (claim_source_url ~ '^https://[^[:space:]]+$'
         AND claim_reviewed_by IS NOT NULL AND claim_reviewed_at IS NOT NULL))
);
CREATE FUNCTION birdtie_validate_business_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.account_id AND account_type='business') THEN
        RAISE EXCEPTION 'Business requires a distinct business principal account';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER businesses_validate_principal BEFORE INSERT OR UPDATE ON businesses
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_business_principal();

CREATE TABLE business_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id),
    user_account_id uuid NOT NULL REFERENCES accounts(id),
    role text NOT NULL CHECK(role IN ('owner','admin','member')),
    status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','invited','removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(business_id,user_account_id)
);
CREATE UNIQUE INDEX business_one_active_owner ON business_memberships(business_id)
    WHERE role='owner' AND status='active';
CREATE FUNCTION birdtie_validate_business_member() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.user_account_id
                  AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Business membership requires active Person';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER business_membership_validate BEFORE INSERT OR UPDATE ON business_memberships
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_business_member();

-- Verified operation is a reviewed relationship, not a category or coordinate inference.
CREATE TABLE business_venue_relations (
    business_id uuid NOT NULL REFERENCES businesses(id),
    place_id uuid NOT NULL REFERENCES venues(place_id),
    status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','verified','revoked')),
    evidence_url text NOT NULL CHECK(evidence_url ~ '^https://[^[:space:]]+$'),
    submitted_by uuid NOT NULL REFERENCES accounts(id),
    reviewed_by uuid REFERENCES accounts(id),
    reviewed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(business_id,place_id),
    CONSTRAINT business_venue_review CHECK
        ((status='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL) OR
         (status IN ('verified','revoked') AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL
          AND reviewed_by<>submitted_by))
);
CREATE INDEX business_venue_by_place ON business_venue_relations(place_id)
    WHERE status='verified';

CREATE FUNCTION birdtie_verified_business_venue(target_business uuid,target_place uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM business_venue_relations r
      JOIN venues v ON v.place_id=r.place_id
      JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved'
      JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id
      JOIN cities c ON c.id=p.city_id
      WHERE r.business_id=target_business AND r.place_id=target_place
        AND r.status='verified' AND v.expires_at>now()
        AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>now())
        AND c.publication_status='published');
$$;

ALTER TABLE activity_organizers ADD COLUMN business_id uuid REFERENCES businesses(id);
ALTER TABLE activity_organizers DROP CONSTRAINT activity_organizer_exactly_one;
ALTER TABLE activity_organizers ADD CONSTRAINT activity_organizer_exactly_one
    CHECK(num_nonnulls(person_account_id,community_id,organization_id,business_id)=1);
CREATE INDEX activity_organizers_business ON activity_organizers(business_id)
    WHERE business_id IS NOT NULL;

CREATE OR REPLACE FUNCTION birdtie_validate_activity_organizer() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE activity_organization uuid; activity_host uuid; activity_place uuid;
BEGIN
    SELECT organization_id,host_account_id,place_id
        INTO activity_organization,activity_host,activity_place
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
    IF NEW.business_id IS NOT NULL AND
       (activity_organization IS NOT NULL OR NOT EXISTS(
          SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
          WHERE b.id=NEW.business_id AND b.account_id=activity_host
            AND b.status='active' AND b.claim_status='verified' AND a.status='active'
            AND (activity_place IS NULL OR birdtie_verified_business_venue(b.id,activity_place)))) THEN
        RAISE EXCEPTION 'business organizer requires verified principal and venue relation';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_initial_activity_organizer() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE mapped_organization uuid; mapped_business uuid;
BEGIN
    mapped_organization := NEW.organization_id;
    IF mapped_organization IS NULL THEN
        SELECT id INTO mapped_organization FROM organizations WHERE account_id=NEW.host_account_id;
    END IF;
    IF mapped_organization IS NOT NULL THEN
        INSERT INTO activity_organizers(activity_id,organization_id)
        VALUES(NEW.id,mapped_organization);
    ELSE
        SELECT id INTO mapped_business FROM businesses WHERE account_id=NEW.host_account_id;
        IF mapped_business IS NOT NULL THEN
            INSERT INTO activity_organizers(activity_id,business_id)
            VALUES(NEW.id,mapped_business);
        ELSE
            INSERT INTO activity_organizers(activity_id,person_account_id)
            VALUES(NEW.id,NEW.host_account_id);
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_validate_activity_organization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE organization_account uuid; creator_type text; business_id uuid;
BEGIN
    IF NEW.organization_id IS NOT NULL THEN
        SELECT account_id INTO organization_account FROM organizations WHERE id=NEW.organization_id;
        IF organization_account IS NULL OR NEW.host_account_id IS DISTINCT FROM organization_account THEN
            RAISE EXCEPTION 'organization activity requires its organization principal as host';
        END IF;
    END IF;
    IF NEW.created_by_account_id IS NOT NULL THEN
        SELECT account_type INTO creator_type FROM accounts WHERE id=NEW.created_by_account_id;
        IF creator_type IS DISTINCT FROM 'person' THEN
            RAISE EXCEPTION 'activity creator must be a person account';
        END IF;
    END IF;
    SELECT b.id INTO business_id FROM businesses b WHERE b.account_id=NEW.host_account_id;
    IF business_id IS NOT NULL AND (NEW.organization_id IS NOT NULL OR NOT EXISTS(
        SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
        WHERE b.id=business_id AND b.status='active' AND b.claim_status='verified'
          AND a.status='active' AND (NEW.place_id IS NULL OR
              birdtie_verified_business_venue(b.id,NEW.place_id)))) THEN
        RAISE EXCEPTION 'business Activity requires verified principal and venue relation';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION birdtie_activity_visible_to(target_activity uuid,viewer uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id
      LEFT JOIN businesses biz ON biz.id=ao.business_id
      LEFT JOIN accounts principal ON principal.id=biz.account_id
      WHERE a.id=target_activity
        AND (ao.business_id IS NULL OR (biz.status='active' AND biz.claim_status='verified'
          AND principal.status='active'
          AND (a.place_id IS NULL OR birdtie_verified_business_venue(biz.id,a.place_id))))
        AND (a.visibility='public' OR (viewer IS NOT NULL AND (
          (a.visibility='organizer_members' AND (
            EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=ao.community_id
                   AND m.user_account_id=viewer AND m.status='active') OR
            EXISTS(SELECT 1 FROM organization_memberships m WHERE m.organization_id=ao.organization_id
                   AND m.user_account_id=viewer AND m.status='active') OR
            EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=ao.business_id
                   AND m.user_account_id=viewer AND m.status='active'))) OR
          (a.visibility='invite_only' AND (
            ao.person_account_id=viewer OR
            EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=ao.community_id
                   AND m.user_account_id=viewer AND m.status='active' AND m.role IN ('owner','admin')) OR
            EXISTS(SELECT 1 FROM organization_memberships m WHERE m.organization_id=ao.organization_id
                   AND m.user_account_id=viewer AND m.status='active' AND m.role IN ('owner','admin')) OR
            EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=ao.business_id
                   AND m.user_account_id=viewer AND m.status='active' AND m.role IN ('owner','admin')) OR
            EXISTS(SELECT 1 FROM activity_invitations i WHERE i.activity_id=a.id
                   AND i.invitee_account_id=viewer AND i.status='invited')))
        ))));
$$;

COMMIT;
