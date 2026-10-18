BEGIN;

-- Downgrade only when the old storage contract can represent every original
-- association. Never delete a link or manufacture a Moment City to achieve it.
LOCK TABLE moment_activity_links IN ACCESS EXCLUSIVE MODE;
LOCK TABLE moments IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM moment_activity_links l
        JOIN moments m ON m.id=l.moment_id
        WHERE m.city_id IS DISTINCT FROM l.city_id
    ) THEN
        RAISE EXCEPTION 'cannot roll back migration 181: Activity associations require independent Moment locations'
            USING ERRCODE = '23514';
    END IF;
END $$;

ALTER TABLE moment_activity_links
    DROP CONSTRAINT moment_activity_links_moment_id_fkey,
    ADD CONSTRAINT moment_activity_links_moment_id_city_id_fkey
        FOREIGN KEY (moment_id, city_id) REFERENCES moments(id, city_id) ON DELETE CASCADE;

COMMIT;
