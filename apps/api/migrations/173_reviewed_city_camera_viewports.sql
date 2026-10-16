-- Display-only city camera hints, not Places, administrative centroids, user
-- locations, content anchors, or permission changes. Reuses the current
-- Mapbox/WGS84 renderer for these two reviewed cities only.
-- Source: Natural Earth 10m populated_places_simple, public domain,
-- commit 789c9904087846cc3361302857aa2e76b0ae71ff; GeoJSON CRS84 + WGS84 .prj.
-- Exact provenance, government extent cross-checks and deployment preflight:
-- docs/migration/REVIEWED-CITY-CAMERA-VIEWPORTS-2026-10-16.md
-- Zoom 11 is a browsing choice, not a source administrative boundary.
WITH reviewed(id, name, source_ref, latitude, longitude) AS (VALUES
    ('civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b', '上海市',
     'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',
     31.218398::double precision, 121.434559::double precision),
    ('civu-geo-7af76973-a56d-4614-a20b-d5934f9ffe9d', '汕头市',
     'civu:geo_area:7af76973-a56d-4614-a20b-d5934f9ffe9d',
     23.371952::double precision, 116.668080::double precision)
)
UPDATE cities c SET
    map_provider = 'mapbox',
    map_center_latitude = r.latitude,
    map_center_longitude = r.longitude,
    map_default_zoom = 11,
    map_viewport_source_ref = 'https://raw.githubusercontent.com/nvkelso/natural-earth-vector/789c9904087846cc3361302857aa2e76b0ae71ff/geojson/ne_10m_populated_places_simple.geojson'
FROM reviewed r
WHERE c.id = r.id AND c.name = r.name AND c.country_code = 'CN'
    AND c.source_ref = r.source_ref AND c.source_label = 'Civu 城市目录'
    AND c.publication_status = 'published'
    AND c.map_provider IS NULL AND c.map_center_latitude IS NULL
    AND c.map_center_longitude IS NULL AND c.map_default_zoom IS NULL
    AND c.map_viewport_source_ref IS NULL;
