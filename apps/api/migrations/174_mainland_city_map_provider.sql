-- Correct only the two unchanged display cameras introduced by migration 173.
-- Canonical camera/content coordinates stay WGS84. The AMap renderer owns its
-- WGS84/GCJ-02 boundary; this migration does not transform content or create POIs.
-- Exact identity, provenance and camera guards preserve later reviewed edits.
WITH reviewed(id, name, source_ref, latitude, longitude) AS (VALUES
    ('civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b', '上海市',
     'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',
     31.218398::double precision, 121.434559::double precision),
    ('civu-geo-7af76973-a56d-4614-a20b-d5934f9ffe9d', '汕头市',
     'civu:geo_area:7af76973-a56d-4614-a20b-d5934f9ffe9d',
     23.371952::double precision, 116.668080::double precision)
)
UPDATE cities c SET map_provider = 'amap'
FROM reviewed r
WHERE c.id = r.id AND c.name = r.name AND c.country_code = 'CN'
    AND c.source_ref = r.source_ref AND c.source_label = 'Civu 城市目录'
    AND c.publication_status = 'published'
    AND c.map_provider = 'mapbox'
    AND c.map_center_latitude = r.latitude
    AND c.map_center_longitude = r.longitude
    AND c.map_default_zoom = 11
    AND c.map_viewport_source_ref = 'https://raw.githubusercontent.com/nvkelso/natural-earth-vector/789c9904087846cc3361302857aa2e76b0ae71ff/geojson/ne_10m_populated_places_simple.geojson';
