BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM places WHERE address_label IS NOT NULL) OR
       EXISTS (SELECT 1 FROM place_candidates WHERE address_label IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove reviewed Place addresses while sourced data exists';
    END IF;
END $$;
ALTER TABLE places DROP COLUMN address_label;
ALTER TABLE place_candidates DROP COLUMN address_label;
COMMIT;
