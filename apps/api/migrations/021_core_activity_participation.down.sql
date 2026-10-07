-- Development rollback only. Refuse to discard any data written to the new
-- fields or table. Run with psql -v ON_ERROR_STOP=1 after taking a backup.
BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM activity_participations) OR
       EXISTS (SELECT 1 FROM activities WHERE
           organization_id IS NOT NULL OR created_by_account_id IS NOT NULL OR
           description <> '' OR category_code IS NOT NULL OR capacity IS NOT NULL OR
           price_minor <> 0 OR currency IS NOT NULL OR eligibility <> '' OR
           language_code IS NOT NULL OR visibility <> 'public' OR published_at IS NOT NULL) OR
       EXISTS (SELECT 1 FROM organizations WHERE
           slug IS NOT NULL OR description <> '' OR official_links <> '[]'::jsonb OR
           verification_status <> 'unverified' OR visibility <> 'public') THEN
        RAISE EXCEPTION '021 rollback refused: new schema contains data';
    END IF;
END $$;

DROP TABLE activity_participations;
DROP FUNCTION birdtie_validate_activity_participant();
DROP TRIGGER activities_validate_organization ON activities;
DROP FUNCTION birdtie_validate_activity_organization();
DROP INDEX activities_organization_recent;
ALTER TABLE activities
    DROP CONSTRAINT activities_cancelled_published,
    DROP CONSTRAINT activities_visibility,
    DROP CONSTRAINT activities_price_nonnegative,
    DROP CONSTRAINT activities_capacity_positive,
    DROP COLUMN published_at,
    DROP COLUMN visibility,
    DROP COLUMN language_code,
    DROP COLUMN eligibility,
    DROP COLUMN currency,
    DROP COLUMN price_minor,
    DROP COLUMN capacity,
    DROP COLUMN category_code,
    DROP COLUMN description,
    DROP COLUMN created_by_account_id,
    DROP COLUMN organization_id;
DROP INDEX organizations_slug_unique;
ALTER TABLE organizations
    DROP CONSTRAINT organizations_official_links_array,
    DROP CONSTRAINT organizations_visibility,
    DROP CONSTRAINT organizations_verification_status,
    DROP CONSTRAINT organizations_slug_format,
    DROP COLUMN visibility,
    DROP COLUMN verification_status,
    DROP COLUMN official_links,
    DROP COLUMN description,
    DROP COLUMN slug;
COMMIT;
