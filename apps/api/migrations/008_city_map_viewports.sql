-- Provider selection and display-only map viewport for published Cities.
-- The viewport is a UI hint, never a Place, a user location, or a Geo claim.

ALTER TABLE cities ADD COLUMN IF NOT EXISTS map_provider text;
ALTER TABLE cities ADD COLUMN IF NOT EXISTS map_center_latitude double precision;
ALTER TABLE cities ADD COLUMN IF NOT EXISTS map_center_longitude double precision;
ALTER TABLE cities ADD COLUMN IF NOT EXISTS map_default_zoom double precision;
ALTER TABLE cities ADD COLUMN IF NOT EXISTS map_viewport_source_ref text;

DO $$ BEGIN
    ALTER TABLE cities ADD CONSTRAINT cities_map_provider_valid
        CHECK (map_provider IS NULL OR map_provider IN ('mapbox', 'amap'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    ALTER TABLE cities ADD CONSTRAINT cities_map_viewport_complete
        CHECK ((map_center_latitude IS NULL AND map_center_longitude IS NULL
                AND map_default_zoom IS NULL AND map_viewport_source_ref IS NULL)
            OR (map_provider IS NOT NULL AND map_center_latitude IS NOT NULL
                AND map_center_longitude IS NOT NULL AND map_default_zoom IS NOT NULL
                AND nullif(map_viewport_source_ref, '') IS NOT NULL));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    ALTER TABLE cities ADD CONSTRAINT cities_map_viewport_ranges
        CHECK ((map_center_latitude IS NULL OR map_center_latitude BETWEEN -90 AND 90)
            AND (map_center_longitude IS NULL OR map_center_longitude BETWEEN -180 AND 180)
            AND (map_default_zoom IS NULL OR map_default_zoom BETWEEN 1 AND 18));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- A documented city-centre view anchor at Aberdeen Town House. It is not a
-- canonical City centroid or a published Place coordinate.
UPDATE cities SET map_provider = 'mapbox',
    map_center_latitude = 57.147861476057,
    map_center_longitude = -2.0952408491127,
    map_default_zoom = 12,
    map_viewport_source_ref = 'https://www.aberdeencity.gov.uk/AAGM/plan-your-visit/town-house-archives'
WHERE id = 'aberdeen-gb' AND map_provider IS NULL;
