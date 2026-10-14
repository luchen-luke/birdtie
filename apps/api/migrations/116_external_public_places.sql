BEGIN;

-- A licensed public-source library is separate from the reviewed Place catalog.
-- Freshness deadlines never delete these records or private reply snapshots.
CREATE TABLE external_public_places (
 provider text NOT NULL CHECK(provider='openstreetmap'),
 provider_place_id text NOT NULL CHECK(provider_place_id ~ '^node/[1-9][0-9]{0,18}$'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 license_url text NOT NULL CHECK(license_url='https://opendatacommons.org/licenses/odbl/1-0/'),
 license_version text NOT NULL CHECK(license_version='ODbL-1.0'),
 attribution text NOT NULL CHECK(attribution='© OpenStreetMap contributors (ODbL 1.0)'),
 review_status text NOT NULL DEFAULT 'UNREVIEWED' CHECK(review_status='UNREVIEWED'),
 observed_at timestamptz NOT NULL,
 fresh_until timestamptz NOT NULL CHECK(fresh_until>observed_at),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 PRIMARY KEY(provider,provider_place_id),
 CHECK(COALESCE(payload->>'cityId','')=''),
 CHECK(payload->>'id'='osm:'||replace(provider_place_id,'/',':')),
 CHECK(payload->>'reviewStatus'='UNREVIEWED')
);

-- Each complete bounded city search is a reproducible public snapshot. Keeping
-- its snapshot also prevents another city's refresh from changing membership.
CREATE TABLE external_public_place_search_cache (
 cache_key text PRIMARY KEY CHECK(length(cache_key) BETWEEN 1 AND 512),
 city_id text NOT NULL REFERENCES cities(id),
 provider text NOT NULL CHECK(provider='openstreetmap'),
 category_code text NOT NULL CHECK(category_code='restaurant'),
 center_latitude double precision NOT NULL CHECK(center_latitude BETWEEN -90 AND 90),
 center_longitude double precision NOT NULL CHECK(center_longitude BETWEEN -180 AND 180),
 radius_meters integer NOT NULL CHECK(radius_meters BETWEEN 1 AND 6000),
 policy_version text NOT NULL CHECK(length(policy_version) BETWEEN 1 AND 100),
 status text NOT NULL CHECK(status IN ('SUCCESS','EMPTY')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 observed_at timestamptz NOT NULL,
 fresh_until timestamptz NOT NULL CHECK(fresh_until>observed_at),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 CHECK(payload->>'schema'='birdtie-online-places-v2'),
 CHECK(payload->>'cityId'=city_id),
 CHECK(payload->>'status'=status),
 CHECK(jsonb_typeof(payload->'places')='array' AND jsonb_array_length(payload->'places')<=15),
 CHECK(COALESCE(payload->>'query','')='' AND COALESCE(payload->>'requestId','')=''),
 CHECK(COALESCE(payload->>'taskId','')='' AND COALESCE(payload->>'snapshotId','')='' AND COALESCE(payload->>'turnDigest','')='')
);
CREATE INDEX external_public_place_search_city ON external_public_place_search_cache(city_id,category_code,provider);

COMMENT ON TABLE external_public_places IS 'ODbL attributed public source facts, unreviewed and never native published Places; freshness expiry is not deletion';
COMMENT ON TABLE external_public_place_search_cache IS 'Public city/category cache only; no user query, owner, session or private Task association';
COMMIT;
