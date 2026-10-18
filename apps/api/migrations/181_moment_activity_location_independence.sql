BEGIN;

-- A Moment's explicit locations are owned by moment_location_links (160).
-- An Activity association records the Activity's own City; it must not invent
-- a CITY location on a Place-only or multi-location Moment. Preserve every
-- existing row and the independent (activity_id, city_id) foreign key.
ALTER TABLE moment_activity_links
    DROP CONSTRAINT moment_activity_links_moment_id_city_id_fkey,
    ADD CONSTRAINT moment_activity_links_moment_id_fkey
        FOREIGN KEY (moment_id) REFERENCES moments(id) ON DELETE CASCADE;

COMMIT;
