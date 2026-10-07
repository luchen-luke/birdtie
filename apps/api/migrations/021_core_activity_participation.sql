-- Core organization-published Activity and RSVP storage. Existing City Seed
-- Activities remain valid: new ownership and publishing fields are nullable.
BEGIN;
ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS slug text,
    ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS official_links jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT 'unverified',
    ADD COLUMN IF NOT EXISTS visibility text NOT NULL DEFAULT 'public';

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_slug_format') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_slug_format
            CHECK (slug IS NULL OR slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_verification_status') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_verification_status
            CHECK (verification_status IN ('unverified', 'pending', 'verified', 'rejected'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_visibility') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_visibility
            CHECK (visibility IN ('public', 'private'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'organizations_official_links_array') THEN
        ALTER TABLE organizations ADD CONSTRAINT organizations_official_links_array
            CHECK (jsonb_typeof(official_links) = 'array');
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS organizations_slug_unique
    ON organizations (slug) WHERE slug IS NOT NULL;

ALTER TABLE activities
    ADD COLUMN IF NOT EXISTS organization_id uuid REFERENCES organizations(id),
    ADD COLUMN IF NOT EXISTS created_by_account_id uuid REFERENCES accounts(id),
    ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS category_code text,
    ADD COLUMN IF NOT EXISTS capacity integer,
    ADD COLUMN IF NOT EXISTS price_minor integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS currency text,
    ADD COLUMN IF NOT EXISTS eligibility text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS language_code text,
    ADD COLUMN IF NOT EXISTS visibility text NOT NULL DEFAULT 'public',
    ADD COLUMN IF NOT EXISTS published_at timestamptz;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activities_capacity_positive') THEN
        ALTER TABLE activities ADD CONSTRAINT activities_capacity_positive
            CHECK (capacity IS NULL OR capacity > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activities_price_nonnegative') THEN
        ALTER TABLE activities ADD CONSTRAINT activities_price_nonnegative
            CHECK (price_minor >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activities_visibility') THEN
        ALTER TABLE activities ADD CONSTRAINT activities_visibility
            CHECK (visibility IN ('public', 'unlisted', 'private'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activities_cancelled_published') THEN
        ALTER TABLE activities ADD CONSTRAINT activities_cancelled_published
            CHECK (cancelled_at IS NULL OR publication_status = 'published');
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS activities_organization_recent
    ON activities (organization_id, created_at DESC, id DESC)
    WHERE organization_id IS NOT NULL;

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
DROP TRIGGER IF EXISTS activities_validate_organization ON activities;
CREATE TRIGGER activities_validate_organization BEFORE INSERT OR UPDATE
    ON activities FOR EACH ROW EXECUTE FUNCTION birdtie_validate_activity_organization();

-- One row per person and Activity, including cancelled RSVPs. Rejoining updates
-- that row instead of creating another participation or confusing it with a plan.
CREATE TABLE IF NOT EXISTS activity_participations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    participant_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'going'
        CHECK (status IN ('going', 'pending', 'cancelled')),
    cancelled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT activity_participation_unique_person_activity
        UNIQUE (activity_id, participant_account_id),
    CONSTRAINT activity_participation_cancel_state
        CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS activity_participations_person_recent
    ON activity_participations (participant_account_id, updated_at DESC, id DESC);

CREATE OR REPLACE FUNCTION birdtie_validate_activity_participant() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts
                   WHERE id = NEW.participant_account_id AND account_type = 'person') THEN
        RAISE EXCEPTION 'activity participant must be a person account';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS activity_participations_validate_person ON activity_participations;
CREATE TRIGGER activity_participations_validate_person BEFORE INSERT OR UPDATE
    ON activity_participations FOR EACH ROW EXECUTE FUNCTION birdtie_validate_activity_participant();
COMMIT;
