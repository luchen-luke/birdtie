-- Read-only, against the already identity-checked Birdtie independent DB.
-- Expected before first application: exactIdentities=2, eligibleMissingMaps=2.
-- Existing configured rows must be preserved, never cleared to force a match.
WITH expected(id, name, source_ref) AS (VALUES
 ('civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b','上海市','civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b'),
 ('civu-geo-7af76973-a56d-4614-a20b-d5934f9ffe9d','汕头市','civu:geo_area:7af76973-a56d-4614-a20b-d5934f9ffe9d')
), checked AS (
 SELECT c.id,
  c.name=e.name AND c.country_code='CN' AND c.source_ref=e.source_ref
   AND c.source_label='Civu 城市目录' AND c.publication_status='published' AS exact_identity,
  c.map_provider IS NULL AND c.map_center_latitude IS NULL
   AND c.map_center_longitude IS NULL AND c.map_default_zoom IS NULL
   AND c.map_viewport_source_ref IS NULL AS missing_map
 FROM expected e LEFT JOIN cities c ON c.id=e.id
)
SELECT jsonb_build_object('expectedCities',2,
 'presentCities',count(id),'exactIdentities',count(*) FILTER(WHERE exact_identity),
 'eligibleMissingMaps',count(*) FILTER(WHERE exact_identity AND missing_map),
 'configuredOrPartial',count(*) FILTER(WHERE id IS NOT NULL AND NOT missing_map),
 'identityMismatches',count(*) FILTER(WHERE id IS NOT NULL AND NOT exact_identity))
FROM checked;
