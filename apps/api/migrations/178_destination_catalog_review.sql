BEGIN;
-- A bounded catalog projection quarantine, not a deletion or fixture verdict.
-- These imported labels also occur as street-address fragments in ten public
-- OSM Places in the preserved source. Keep every City, Place and original
-- migration/alias record available to its existing direct read paths.
CREATE TABLE IF NOT EXISTS destination_catalog_exclusions (
 city_id text PRIMARY KEY REFERENCES cities(id) ON DELETE RESTRICT,
 run_id uuid NOT NULL REFERENCES legacy_native_adaptation_runs(run_id),
 source_geo_id uuid NOT NULL,
 expected_source_ref text NOT NULL,
 expected_country_code char(2) NOT NULL CHECK(expected_country_code ~ '^[A-Z]{2}$'),
 expected_name text NOT NULL CHECK(length(expected_name) BETWEEN 1 AND 160),
 expected_city_updated_at timestamptz NOT NULL,
 reason text NOT NULL CHECK(reason='UNVERIFIED_IMPORTED_CITY_LABEL'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256 ~ '^[0-9a-f]{64}$'),
 source_row_sha256 text NOT NULL CHECK(source_row_sha256 ~ '^[0-9a-f]{64}$'),
 evidence_reference text NOT NULL CHECK(length(evidence_reference) BETWEEN 1 AND 512),
 active boolean NOT NULL DEFAULT true,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 reviewed_at timestamptz,
 review_note text NOT NULL DEFAULT '',
 CHECK(city_id='civu-geo-'||source_geo_id::text),
 CHECK(expected_source_ref='civu:geo_area:'||source_geo_id::text)
);

-- Exact identities only. The preserved source SHA was independently checked;
-- no numeric-name pattern, country-wide exclusion or name-based merge is used.
-- Current City facts, same-run alias and immutable native source proof must all
-- agree before insertion. Re-running never changes a later review decision.
WITH reviewed(geo,name) AS (VALUES
 ('322091f7-339c-42bc-98a6-cc10d3c4b143'::uuid,'100b'),
 ('442af423-c66d-47c4-8a3d-c2fcd997bbb0'::uuid,'20a'),
 ('7b571203-b218-4a5f-b0ae-9c8dce2972e3'::uuid,'22a'),
 ('8d2ef2af-ef82-4400-a3bc-89c031b382cd'::uuid,'23B'),
 ('64103107-84a2-4e1d-ae6f-482910dc9339'::uuid,'2a'),
 ('098f8e6f-1035-497d-8bd3-69b6da6c3395'::uuid,'38a'),
 ('2acfbe70-cb0b-4756-8e2c-98d810994097'::uuid,'6A'),
 ('3aa8cd85-1600-4dce-938c-12a178a24a46'::uuid,'74a')
)
INSERT INTO destination_catalog_exclusions(
 city_id,run_id,source_geo_id,expected_source_ref,expected_country_code,
 expected_name,expected_city_updated_at,reason,source_file_sha256,
 source_row_sha256,evidence_reference
)
SELECT c.id,n.run_id,r.geo,c.source_ref,c.country_code,c.name,c.updated_at,
 'UNVERIFIED_IMPORTED_CITY_LABEL',original.source_file_sha256,e.source_sha256,
 'source.json:tables.geo_areas/'||r.geo::text||'; linked public Place city/address and OSM provider references; data-source-provenance.json'
FROM reviewed r
JOIN cities c ON c.id='civu-geo-'||r.geo::text
 AND c.name=r.name AND c.country_code='IS'
 AND c.source_ref='civu:geo_area:'||r.geo::text
 AND c.publication_status='published'
JOIN legacy_native_catalog_aliases n ON n.kind='city'
 AND n.source_id=r.geo AND n.target_city_id=c.id
JOIN legacy_native_adaptation_entities e ON e.run_id=n.run_id
 AND e.kind='city' AND e.source_key=r.geo::text AND e.target_id=c.id
 AND e.owner_account_id IS NULL
JOIN legacy_import_runs original ON original.run_id=n.run_id
 AND original.source_file_sha256='ba6b9b5d2d404ada806a858cbc0931628a5958c4b2abdbd8b7d50b62b4e6f40d'
WHERE n.run_id='c5df0d7c-f361-4275-9838-feb6d1d92352'
ON CONFLICT(city_id) DO NOTHING;
COMMIT;
