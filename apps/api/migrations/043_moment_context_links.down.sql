BEGIN;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM moment_community_links) OR
       EXISTS (SELECT 1 FROM moment_organization_links) THEN
        RAISE EXCEPTION 'cannot remove Moment context links while user data exists';
    END IF;
END $$;
DROP TABLE moment_organization_links;
DROP TABLE moment_community_links;

COMMIT;
