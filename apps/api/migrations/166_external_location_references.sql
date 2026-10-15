BEGIN;
CREATE TABLE location_references(
 id uuid PRIMARY KEY,provider text NOT NULL CHECK(provider='openstreetmap'),object_kind text NOT NULL CHECK(object_kind IN('node','way','relation')),
 object_id text NOT NULL CHECK(object_id ~ '^[1-9][0-9]{0,19}$'),status text NOT NULL DEFAULT 'ACTIVE' CHECK(status IN('ACTIVE','RETIRED')),
 UNIQUE(provider,object_kind,object_id)
);
CREATE TABLE location_reference_observations(
 reference_id uuid PRIMARY KEY REFERENCES location_references(id),version text NOT NULL CHECK(version ~ '^[a-f0-9]{64}$'),
 name text NOT NULL,address text NOT NULL,region text NOT NULL,country_code text NOT NULL,source_url text NOT NULL,attribution text NOT NULL,
 coordinate_system text NOT NULL CHECK(coordinate_system='wgs84'),latitude double precision NOT NULL CHECK(latitude BETWEEN -90 AND 90),longitude double precision NOT NULL CHECK(longitude BETWEEN -180 AND 180),
 observed_at timestamptz NOT NULL,valid_until timestamptz NOT NULL CHECK(valid_until>observed_at AND valid_until<=observed_at+interval '24 hours')
);
ALTER TABLE moment_location_links ADD COLUMN reference_id uuid REFERENCES location_references(id);
ALTER TABLE moment_location_links DROP CONSTRAINT moment_location_links_kind_check;
ALTER TABLE moment_location_links DROP CONSTRAINT moment_location_links_check;
ALTER TABLE moment_location_links ADD CONSTRAINT moment_location_kind CHECK(kind IN('CITY','PLACE','REFERENCE'));
ALTER TABLE moment_location_links ADD CONSTRAINT moment_location_shape CHECK(
 (kind='CITY' AND city_id IS NOT NULL AND place_id IS NULL AND reference_id IS NULL AND precision IN('none','city')) OR
 (kind='PLACE' AND city_id IS NULL AND place_id IS NOT NULL AND reference_id IS NULL) OR
 (kind='REFERENCE' AND city_id IS NULL AND place_id IS NULL AND reference_id IS NOT NULL AND precision IN('none','place')));
CREATE UNIQUE INDEX moment_location_active_reference ON moment_location_links(moment_id,reference_id) WHERE status='ACTIVE' AND kind='REFERENCE';
CREATE OR REPLACE FUNCTION birdtie_guard_moment_location() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Moment location tombstone required';END IF;
 IF TG_OP='UPDATE' AND(OLD.status='REMOVED' OR NEW.id<>OLD.id OR NEW.moment_id<>OLD.moment_id OR NEW.kind<>OLD.kind OR NEW.city_id IS DISTINCT FROM OLD.city_id OR NEW.place_id IS DISTINCT FROM OLD.place_id OR NEW.reference_id IS DISTINCT FROM OLD.reference_id OR NEW.revision<>OLD.revision+1)THEN RAISE EXCEPTION 'stable Moment location identity required';END IF;
 RETURN NEW;END $$;
CREATE FUNCTION birdtie_location_reference_source(rid uuid,at_time timestamptz) RETURNS TABLE(reference_id uuid,name text,address text,region text,country_code text,source_url text,attribution text,latitude double precision,longitude double precision,binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT r.id,o.name,o.address,o.region,o.country_code,o.source_url,o.attribution,o.latitude,o.longitude,jsonb_build_array(r.xmin::text,o.version,o.observed_at,o.valid_until)::text,o.valid_until
 FROM location_references r JOIN location_reference_observations o ON o.reference_id=r.id WHERE r.id=rid AND r.status='ACTIVE' AND o.observed_at<=at_time AND o.valid_until>at_time
$$;
CREATE FUNCTION birdtie_external_location_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'external-location-reference-v1'::text$$;
COMMIT;
