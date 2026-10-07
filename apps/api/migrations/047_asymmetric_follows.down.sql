BEGIN;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM follows) THEN
        RAISE EXCEPTION 'cannot remove Follow relationships while data exists';
    END IF;
END $$;
DROP TRIGGER account_block_removes_follows ON account_blocks;
DROP FUNCTION birdtie_block_removes_follows();
DROP TRIGGER follows_validate ON follows;
DROP FUNCTION birdtie_validate_follow();
DROP TABLE follows;

COMMIT;
