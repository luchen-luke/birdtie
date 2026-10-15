BEGIN;
-- Read visibility and coordinate provenance only. No publication or account
-- activation, no changes to preserved source, existing receipts or native text.
CREATE TABLE legacy_map_visibility_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_import_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-map-visibility-v1'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[0-9a-f]{64}$'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(birdtie_legacy_nonnegative_counts(counts)),
 deferred jsonb NOT NULL CHECK(birdtie_legacy_nonnegative_counts(deferred)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_map_place_facts(
 place_id uuid PRIMARY KEY REFERENCES places(id),run_id uuid NOT NULL REFERENCES legacy_map_visibility_runs(run_id) DEFERRABLE INITIALLY DEFERRED,
 source_place_id uuid NOT NULL,source_sha256 text NOT NULL CHECK(source_sha256~'^[0-9a-f]{64}$'),
 public_catalog boolean NOT NULL,
 coordinate_proof text NOT NULL CHECK(coordinate_proof IN('EXPLICIT_STORED_WGS84_V1','OSM_PROVIDER_IDENTITY_PROMOTION_V1','UNPROVEN','CONFLICTING')),
 latitude double precision,longitude double precision,
 native_binding text CHECK(native_binding IS NULL OR native_binding~'^[0-9a-f]{64}$'),
 UNIQUE(run_id,source_place_id),
 CHECK((coordinate_proof IN('UNPROVEN','CONFLICTING') AND latitude IS NULL AND longitude IS NULL)
 OR(coordinate_proof NOT IN('UNPROVEN','CONFLICTING') AND latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180 AND latitude IS NOT NULL AND longitude IS NOT NULL))
);
CREATE FUNCTION birdtie_legacy_map_place_binding(p places)RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(p.id,p.city_id,p.name,p.category_code,p.summary,p.latitude,p.longitude,p.coordinate_system,p.location_precision,p.publication_status,p.source_label,p.source_ref,p.maintainer_label,p.updated_at,p.expires_at)::text,'UTF8')),'hex')
$$;
CREATE FUNCTION birdtie_legacy_map_binding_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='legacy_map_visibility_runs' THEN
  IF NOT EXISTS(SELECT 1 FROM legacy_import_runs original JOIN legacy_native_adaptation_runs native USING(run_id) JOIN legacy_native_context_runs contexts USING(run_id)
   WHERE original.run_id=NEW.run_id AND original.source_file_sha256=NEW.source_file_sha256 AND original.manifest_file_sha256=NEW.manifest_file_sha256 AND original.snapshot_sha256=NEW.snapshot_sha256)
  OR(SELECT count(*) FROM legacy_map_place_facts WHERE run_id=NEW.run_id)<>(NEW.counts->>'places')::integer
  OR(SELECT count(*) FROM legacy_map_place_facts WHERE run_id=NEW.run_id AND public_catalog)<>(NEW.counts->>'publicPlaces')::integer
  OR(SELECT count(*) FROM legacy_map_place_facts WHERE run_id=NEW.run_id AND latitude IS NOT NULL)<>(NEW.counts->>'coordinatePlaces')::integer
  OR(SELECT count(*) FROM legacy_note_archives WHERE run_id=NEW.run_id AND NOT deleted AND source_visibility='public' AND source_moderation_status='ok')<>(NEW.counts->>'publicMoments')::integer
  THEN RAISE EXCEPTION 'exact complete original map source required'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM places p JOIN(
   SELECT e.run_id,e.source_key,e.source_sha256,e.target_id,r.xmin import_xid FROM legacy_native_adaptation_entities e JOIN legacy_native_adaptation_runs r USING(run_id) WHERE e.kind='place'
   UNION ALL SELECT e.run_id,e.source_key,e.source_sha256,e.target_id::text,r.xmin FROM legacy_native_context_entities e JOIN legacy_native_context_runs r USING(run_id) WHERE e.kind='unplaced_place'
  ) x ON x.target_id=p.id::text WHERE p.id=NEW.place_id AND x.run_id=NEW.run_id AND x.source_key=NEW.source_place_id::text AND x.source_sha256=NEW.source_sha256
  AND((p.xmin=x.import_xid AND NEW.native_binding=birdtie_legacy_map_place_binding(p))OR(p.xmin<>x.import_xid AND NEW.native_binding IS NULL)))
  THEN RAISE EXCEPTION 'exact original native map Place required'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER legacy_map_run_binding AFTER INSERT ON legacy_map_visibility_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_map_binding_guard();
CREATE CONSTRAINT TRIGGER legacy_map_place_binding AFTER INSERT ON legacy_map_place_facts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_map_binding_guard();
CREATE TRIGGER legacy_map_run_immutable BEFORE UPDATE OR DELETE ON legacy_map_visibility_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_map_place_immutable BEFORE UPDATE OR DELETE ON legacy_map_place_facts FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_map_visibility_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'map-content-v1'::text $$;
COMMIT;
