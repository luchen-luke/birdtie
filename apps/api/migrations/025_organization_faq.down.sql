BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM organization_faqs) THEN
        RAISE EXCEPTION '025 rollback refused: organization FAQ data exists';
    END IF;
END $$;
DROP TABLE organization_faqs;
COMMIT;
