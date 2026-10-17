BEGIN;
-- Reviewed display bindings only. Historical archives and private media are
-- immutable and are neither updated nor exposed by this projection.
CREATE TABLE IF NOT EXISTS city_artwork_projections (
 city_id text PRIMARY KEY REFERENCES cities(id),
 run_id uuid NOT NULL,
 source_geo_id uuid NOT NULL,
 source_media_id uuid NOT NULL,
 source_version text NOT NULL CHECK(source_version ~ '^[a-f0-9]{64}$'),
 input_sha256 text NOT NULL CHECK(input_sha256 ~ '^[a-f0-9]{64}$'),
 active boolean NOT NULL DEFAULT true,
 reviewed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(run_id,source_geo_id) REFERENCES legacy_system_artwork_archives(run_id,source_geo_id),
 FOREIGN KEY(run_id,source_media_id) REFERENCES legacy_reference_media_assets(run_id,source_media_id)
);

CREATE OR REPLACE FUNCTION birdtie_city_artwork_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
   SELECT 1 FROM legacy_system_artwork_archives a
   JOIN legacy_native_catalog_aliases n ON n.run_id=a.run_id AND n.kind='city' AND n.source_id=a.source_geo_id
   JOIN legacy_reference_media_assets m ON m.run_id=a.run_id AND m.source_media_id=a.source_media_id
   WHERE a.run_id=NEW.run_id AND a.source_geo_id=NEW.source_geo_id
     AND n.target_city_id=NEW.city_id AND a.source_media_id=NEW.source_media_id
     AND a.source_version=NEW.source_version AND m.input_sha256=NEW.input_sha256
     AND m.system_archive AND m.validation_kind='AUTHENTICATED_SYSTEM_ARCHIVE_ONLY'
     AND m.owner_account_id IS NULL AND m.material_id IS NULL
     AND octet_length(m.original)=m.input_bytes AND encode(sha256(m.original),'hex')=m.input_sha256
 ) THEN RAISE EXCEPTION 'reviewed system city artwork provenance required'; END IF;
 RETURN NEW;
END $$;
DO $$
BEGIN
 IF NOT EXISTS (
   SELECT 1 FROM pg_trigger
   WHERE tgrelid='city_artwork_projections'::regclass
     AND tgname='city_artwork_binding' AND NOT tgisinternal
 ) THEN
   CREATE TRIGGER city_artwork_binding BEFORE INSERT OR UPDATE ON city_artwork_projections
    FOR EACH ROW EXECUTE FUNCTION birdtie_city_artwork_binding();
 END IF;
END $$;

-- Exact reviewed source identities from the authenticated 2026-10-15 export.
-- The third archived system image is York; Edinburgh is not assigned to it.
-- Missing or changed sources remain unbound. Existing bindings always win;
-- re-running this reviewed seed can never downgrade a later city revision.
WITH reviewed(geo,media,version,country) AS (VALUES
 ('7a536573-67c6-42f0-9ede-c423fb5dba00'::uuid,'b768cc63-968c-4f07-9afc-914875b75aad'::uuid,'d2988c88b86453e1ead0c637ffeb85abdcfd89c9ce3894d12351a609b4429988','CN'),
 ('bbffd2af-68b4-49ee-bb01-5e5a7673aa9b'::uuid,'4929ecdc-e7a7-4e97-ba06-850899a01d67'::uuid,'69b9d3d653f3b7f64fd1423c54e18d73cb90f1186a824bb1cfe1319a835a4218','CN')
)
INSERT INTO city_artwork_projections(city_id,run_id,source_geo_id,source_media_id,source_version,input_sha256)
SELECT n.target_city_id,a.run_id,a.source_geo_id,a.source_media_id,a.source_version,m.input_sha256
FROM reviewed r
JOIN legacy_system_artwork_archives a ON a.source_geo_id=r.geo AND a.source_media_id=r.media AND a.source_version=r.version
JOIN legacy_native_catalog_aliases n ON n.run_id=a.run_id AND n.kind='city' AND n.source_id=a.source_geo_id
JOIN legacy_reference_media_assets m ON m.run_id=a.run_id AND m.source_media_id=a.source_media_id
JOIN cities c ON c.id=n.target_city_id AND c.country_code=r.country
WHERE a.run_id='c5df0d7c-f361-4275-9838-feb6d1d92352'
 AND n.target_city_id='civu-geo-'||r.geo::text
 AND m.input_sha256=r.version AND m.system_archive
 AND m.validation_kind='AUTHENTICATED_SYSTEM_ARCHIVE_ONLY'
 AND m.material_id IS NULL AND m.owner_account_id IS NULL
 AND octet_length(m.original)=m.input_bytes AND encode(sha256(m.original),'hex')=m.input_sha256
ON CONFLICT(city_id) DO NOTHING;
COMMIT;
