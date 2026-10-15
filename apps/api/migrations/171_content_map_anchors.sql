BEGIN;
-- Exact legacy display grants only. Original private GPS/context rows and all
-- native publication states stay untouched; these rows never create a Place.
CREATE TABLE legacy_map_anchor_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_map_visibility_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-map-anchors-v1'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[0-9a-f]{64}$'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(birdtie_legacy_nonnegative_counts(counts)),
 deferred jsonb NOT NULL CHECK(birdtie_legacy_nonnegative_counts(deferred)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION birdtie_legacy_map_moment_binding(m moments)RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(m.id,m.author_account_id,m.revision,m.title,m.body,m.status,m.visibility,m.updated_at)::text,'UTF8')),'hex')
$$;
CREATE TABLE legacy_map_anchor_facts(
 id uuid PRIMARY KEY,run_id uuid NOT NULL REFERENCES legacy_map_anchor_runs(run_id) DEFERRABLE INITIALLY DEFERRED,
 moment_id uuid NOT NULL REFERENCES moments(id),context_id uuid REFERENCES private_moment_media_contexts(id),media_id uuid REFERENCES media_assets(id),
 source_note_id uuid NOT NULL,source_note_sha256 text NOT NULL CHECK(source_note_sha256~'^[0-9a-f]{64}$'),source_media_id uuid,
 source_sha256 text NOT NULL CHECK(source_sha256~'^[0-9a-f]{64}$'),
 coordinate_proof text NOT NULL CHECK(coordinate_proof IN('CIVU_EXPLICIT_MEDIA_MAP_WGS84_V1','CIVU_EXPLICIT_NOTE_MAP_WGS84_V1')),
 precision text NOT NULL CHECK(precision IN('point','approximate')),
 latitude double precision NOT NULL CHECK(latitude BETWEEN -90 AND 90),longitude double precision NOT NULL CHECK(longitude BETWEEN -180 AND 180),
 native_binding text NOT NULL CHECK(native_binding~'^[0-9a-f]{64}$'),
 CHECK(latitude<>0 OR longitude<>0),
 CHECK((context_id IS NOT NULL AND media_id IS NOT NULL AND source_media_id IS NOT NULL AND coordinate_proof='CIVU_EXPLICIT_MEDIA_MAP_WGS84_V1' AND precision='point') OR(context_id IS NULL AND media_id IS NULL AND source_media_id IS NULL AND coordinate_proof='CIVU_EXPLICIT_NOTE_MAP_WGS84_V1')),
 UNIQUE(run_id,source_note_id,source_media_id)
);
CREATE FUNCTION birdtie_legacy_map_anchor_guard()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_TABLE_NAME='legacy_map_anchor_runs' THEN
 IF NOT EXISTS(SELECT 1 FROM legacy_import_runs o WHERE o.run_id=NEW.run_id AND o.source_file_sha256=NEW.source_file_sha256 AND o.manifest_file_sha256=NEW.manifest_file_sha256 AND o.snapshot_sha256=NEW.snapshot_sha256)
 OR(SELECT count(*)FROM legacy_map_anchor_facts WHERE run_id=NEW.run_id)<>(NEW.counts->>'anchors')::integer THEN RAISE EXCEPTION 'exact map anchor source required';END IF;
 ELSE
 IF NOT EXISTS(SELECT 1 FROM moments m JOIN legacy_native_adaptation_entities n ON n.kind='moment' AND n.target_id=m.id::text AND n.owner_account_id=m.author_account_id
 JOIN legacy_note_archives a ON a.run_id=n.run_id AND a.source_note_id::text=n.source_key AND a.source_sha256=n.source_sha256 AND a.owner_account_id=n.owner_account_id
 WHERE m.id=NEW.moment_id AND n.run_id=NEW.run_id AND a.source_note_id=NEW.source_note_id AND a.source_sha256=NEW.source_note_sha256 AND NOT a.deleted AND m.revision=1 AND m.status='draft' AND m.visibility='private' AND m.title=a.title AND m.body=a.body AND NEW.native_binding=birdtie_legacy_map_moment_binding(m)) THEN RAISE EXCEPTION 'unchanged original Moment required';END IF;
 IF NEW.context_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM private_moment_media_contexts c JOIN legacy_native_context_entities e ON e.kind='media_context' AND e.target_id=c.id AND e.owner_account_id=c.owner_account_id
 WHERE c.id=NEW.context_id AND c.moment_id=NEW.moment_id AND e.run_id=NEW.run_id AND e.source_parent_key=NEW.source_note_id::text AND e.source_parent_sha256=NEW.source_note_sha256 AND e.source_sha256=NEW.source_sha256 AND c.original_map_enabled AND c.source_latitude::float8=NEW.latitude AND c.source_longitude::float8=NEW.longitude AND c.status='ACTIVE' AND coalesce(c.image_id,c.video_id)=NEW.media_id AND NOT EXISTS(SELECT 1 FROM private_moment_media_context_places p WHERE p.context_id=c.id))THEN RAISE EXCEPTION 'explicit unplaced original media map grant required';END IF;
 END IF;RETURN NEW;END$$;
CREATE CONSTRAINT TRIGGER legacy_map_anchor_run_binding AFTER INSERT ON legacy_map_anchor_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_map_anchor_guard();
CREATE CONSTRAINT TRIGGER legacy_map_anchor_fact_binding AFTER INSERT ON legacy_map_anchor_facts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_map_anchor_guard();
CREATE TRIGGER legacy_map_anchor_runs_immutable BEFORE UPDATE OR DELETE ON legacy_map_anchor_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_map_anchor_facts_immutable BEFORE UPDATE OR DELETE ON legacy_map_anchor_facts FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE INDEX legacy_map_anchor_viewport ON legacy_map_anchor_facts(latitude,longitude,id);
CREATE INDEX legacy_map_place_viewport ON legacy_map_place_facts(latitude,longitude,place_id) WHERE latitude IS NOT NULL;
CREATE INDEX native_place_content_viewport ON places(latitude,longitude,id) WHERE coordinate_system='wgs84' AND location_precision='point' AND publication_status IN('draft','published');
CREATE INDEX moment_location_place_content ON moment_location_links(place_id,moment_id) WHERE status='ACTIVE' AND kind='PLACE';
CREATE INDEX moment_media_context_place_content ON private_moment_media_context_places(place_id,context_id);
CREATE INDEX location_reference_content_viewport ON location_reference_observations(latitude,longitude,reference_id);
CREATE FUNCTION birdtie_content_map_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'map-content-v2'::text$$;
COMMIT;
