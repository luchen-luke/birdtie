BEGIN;
-- Independent preservation; no existing publication, media or import receipt changes.
CREATE TABLE legacy_reference_media_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_import_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-legacy-reference-media-v1'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[0-9a-f]{64}$'),
 matcher_file_sha256 text NOT NULL CHECK(matcher_file_sha256~'^[0-9a-f]{64}$'),
 seal_sha256 text NOT NULL CHECK(seal_sha256~'^[0-9a-f]{64}$'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object' AND birdtie_legacy_nonnegative_counts(counts)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE private_reference_image_materials(
 id uuid PRIMARY KEY, owner_account_id uuid NOT NULL REFERENCES accounts(id),
 backing_image_id uuid REFERENCES private_moment_images(id),
 mime_type text NOT NULL CHECK(mime_type IN('image/jpeg','image/png')),
 byte_size integer NOT NULL CHECK(byte_size BETWEEN 1 AND 16777216),
 sha256 text NOT NULL CHECK(sha256~'^[0-9a-f]{64}$'),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 8192), height integer NOT NULL CHECK(height BETWEEN 1 AND 8192),
 content bytea, status text NOT NULL CHECK(status IN('READY_PRIVATE','DELETED')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(width::bigint*height<=50331648),
 CHECK((status='READY_PRIVATE' AND((backing_image_id IS NOT NULL AND content IS NULL) OR(backing_image_id IS NULL AND octet_length(content)=byte_size AND encode(sha256(content),'hex')=sha256))) OR(status='DELETED' AND content IS NULL))
);
CREATE TABLE legacy_reference_media_assets(
 run_id uuid NOT NULL REFERENCES legacy_reference_media_runs(run_id) DEFERRABLE INITIALLY DEFERRED,
 source_media_id uuid NOT NULL, source_media_sha256 text NOT NULL CHECK(source_media_sha256~'^[0-9a-f]{64}$'),
 source_owner_id uuid, owner_account_id uuid REFERENCES accounts(id),
 material_id uuid UNIQUE REFERENCES private_reference_image_materials(id),
 input_sha256 text NOT NULL CHECK(input_sha256~'^[0-9a-f]{64}$'),input_bytes bigint NOT NULL CHECK(input_bytes BETWEEN 1 AND 16777216),
 validation_kind text NOT NULL CHECK(validation_kind IN('STATIC_IMAGE_DECODE_REENCODE_V1','EXISTING_NATIVE_PRIVATE_DERIVATIVE','AUTHENTICATED_SYSTEM_ARCHIVE_ONLY')),
 antivirus_scan text NOT NULL DEFAULT 'NOT_RUN' CHECK(antivirus_scan='NOT_RUN'),privacy_risk text NOT NULL DEFAULT 'UNKNOWN' CHECK(privacy_risk='UNKNOWN'),
 system_archive boolean NOT NULL, original bytea,
 PRIMARY KEY(run_id,source_media_id),
 CHECK((system_archive AND material_id IS NULL AND owner_account_id IS NULL AND octet_length(original)=input_bytes AND encode(sha256(original),'hex')=input_sha256) OR(NOT system_archive AND material_id IS NOT NULL AND owner_account_id IS NOT NULL AND source_owner_id IS NOT NULL AND original IS NULL))
);
CREATE TABLE legacy_reference_media_relations(
 id uuid PRIMARY KEY,run_id uuid NOT NULL REFERENCES legacy_reference_media_runs(run_id) DEFERRABLE INITIALLY DEFERRED,
 kind text NOT NULL CHECK(kind IN('PLACE_IMAGE','JOURNEY_MEDIA','MOMENT_COVER')),
 source_key uuid NOT NULL, source_sha256 text NOT NULL CHECK(source_sha256~'^[0-9a-f]{64}$'),
 source_parent_id uuid NOT NULL,source_parent_sha256 text NOT NULL CHECK(source_parent_sha256~'^[0-9a-f]{64}$'),
 source_media_id uuid NOT NULL,source_owner_id uuid NOT NULL,owner_account_id uuid NOT NULL REFERENCES accounts(id),
 target_type text NOT NULL CHECK(target_type IN('PLACE','JOURNEY','MOMENT')),target_id uuid,
 source_type text NOT NULL CHECK(length(source_type)<=80),review_status text NOT NULL CHECK(review_status IN('approved','rejected','removed','pending','none')),
 rights_basis text NOT NULL CHECK(length(rights_basis)<=160),attribution text NOT NULL CHECK(octet_length(attribution)<=16000),
 source_note_id uuid,source_position bigint NOT NULL,source_is_cover boolean NOT NULL,source_publishable boolean,
 tombstone boolean NOT NULL,UNIQUE(run_id,kind,source_key),
 CHECK(tombstone OR target_id IS NOT NULL)
);
CREATE TABLE private_reference_image_links(
 id uuid PRIMARY KEY REFERENCES legacy_reference_media_relations(id),
 material_id uuid REFERENCES private_reference_image_materials(id),
 status text NOT NULL CHECK(status IN('ACTIVE','DELETED','TOMBSTONE')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((status='ACTIVE' AND material_id IS NOT NULL)OR(status IN('DELETED','TOMBSTONE')))
);
CREATE TABLE legacy_system_artwork_archives(
 run_id uuid NOT NULL REFERENCES legacy_reference_media_runs(run_id) DEFERRABLE INITIALLY DEFERRED,
 source_geo_id uuid NOT NULL,source_geo_sha256 text NOT NULL CHECK(source_geo_sha256~'^[0-9a-f]{64}$'),
 source_media_id uuid NOT NULL,source_version text NOT NULL,source_owner_id uuid,
 PRIMARY KEY(run_id,source_geo_id),FOREIGN KEY(run_id,source_media_id)REFERENCES legacy_reference_media_assets(run_id,source_media_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE private_reference_image_operations(
 owner_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,
 image_id uuid NOT NULL REFERENCES private_reference_image_links(id),input_sha256 text NOT NULL CHECK(input_sha256~'^[0-9a-f]{64}$'),
 receipt jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(owner_account_id,operation_id)
);
CREATE INDEX reference_image_owner_target ON legacy_reference_media_relations(owner_account_id,target_type,target_id,id);
CREATE FUNCTION birdtie_reference_media_owner_binding()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_entity_mappings u WHERE u.run_id=NEW.run_id AND u.entity_kind='user' AND u.source_id=NEW.source_owner_id AND u.target_id=NEW.owner_account_id AND u.owner_account_id=NEW.owner_account_id)
 THEN RAISE EXCEPTION 'original image owner mapping required'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER reference_media_owner_binding AFTER INSERT ON legacy_reference_media_relations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_reference_media_owner_binding();
CREATE FUNCTION birdtie_reference_media_run_binding()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE physical bigint; reused bigint; relations bigint; tombs bigint; artwork bigint;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_import_runs original WHERE original.run_id=NEW.run_id AND original.source_file_sha256=NEW.source_file_sha256 AND original.manifest_file_sha256=NEW.manifest_file_sha256 AND original.snapshot_sha256=NEW.snapshot_sha256)
 THEN RAISE EXCEPTION 'original source frame required'; END IF;
 SELECT count(*)FILTER(WHERE m.backing_image_id IS NULL),count(*)FILTER(WHERE m.backing_image_id IS NOT NULL) INTO physical,reused FROM legacy_reference_media_assets a JOIN private_reference_image_materials m ON m.id=a.material_id WHERE a.run_id=NEW.run_id;
 SELECT count(*),count(*)FILTER(WHERE tombstone)INTO relations,tombs FROM legacy_reference_media_relations WHERE run_id=NEW.run_id;
 SELECT count(*)INTO artwork FROM legacy_system_artwork_archives WHERE run_id=NEW.run_id;
 IF physical<>(NEW.counts->>'privateImages')::bigint OR reused<>(NEW.counts->>'reusedImages')::bigint OR relations<>(NEW.counts->>'placeRelations')::bigint+(NEW.counts->>'journeyMedia')::bigint+(NEW.counts->>'momentCovers')::bigint OR tombs<>(NEW.counts->>'relationTombstones')::bigint OR artwork<>(NEW.counts->>'systemArtworkArchives')::bigint
 THEN RAISE EXCEPTION 'complete reference media facts required'; END IF;
 IF EXISTS(SELECT 1 FROM legacy_reference_media_assets a LEFT JOIN legacy_entity_mappings u ON u.run_id=a.run_id AND u.entity_kind='user' AND u.source_id=a.source_owner_id AND u.target_id=a.owner_account_id AND u.owner_account_id=a.owner_account_id JOIN private_reference_image_materials m ON m.id=a.material_id WHERE a.run_id=NEW.run_id AND NOT a.system_archive AND(u.target_id IS NULL OR m.owner_account_id<>a.owner_account_id))
 THEN RAISE EXCEPTION 'original material owner required'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER reference_media_run_binding AFTER INSERT ON legacy_reference_media_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_reference_media_run_binding();
CREATE FUNCTION birdtie_reference_link_transition()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.material_id IS DISTINCT FROM OLD.material_id OR OLD.status<>'ACTIVE' OR NEW.status<>'DELETED' OR NEW.revision<>OLD.revision+1 OR NEW.created_at IS DISTINCT FROM OLD.created_at
 THEN RAISE EXCEPTION 'reference image relation transition rejected'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reference_link_transition BEFORE UPDATE OR DELETE ON private_reference_image_links FOR EACH ROW EXECUTE FUNCTION birdtie_reference_link_transition();
CREATE FUNCTION birdtie_reference_material_transition()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.owner_account_id IS DISTINCT FROM OLD.owner_account_id OR NEW.backing_image_id IS DISTINCT FROM OLD.backing_image_id OR NEW.mime_type IS DISTINCT FROM OLD.mime_type OR NEW.byte_size<>OLD.byte_size OR NEW.sha256<>OLD.sha256 OR NEW.width<>OLD.width OR NEW.height<>OLD.height OR NEW.created_at IS DISTINCT FROM OLD.created_at OR OLD.status<>'READY_PRIVATE' OR NEW.status<>'DELETED' OR NEW.revision<>OLD.revision+1 OR NEW.content IS NOT NULL
 THEN RAISE EXCEPTION 'reference image material transition rejected'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reference_material_transition BEFORE UPDATE OR DELETE ON private_reference_image_materials FOR EACH ROW EXECUTE FUNCTION birdtie_reference_material_transition();
CREATE TRIGGER reference_run_immutable BEFORE UPDATE OR DELETE ON legacy_reference_media_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER reference_asset_immutable BEFORE UPDATE OR DELETE ON legacy_reference_media_assets FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER reference_relation_immutable BEFORE UPDATE OR DELETE ON legacy_reference_media_relations FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER reference_artwork_immutable BEFORE UPDATE OR DELETE ON legacy_system_artwork_archives FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER reference_operation_immutable BEFORE UPDATE OR DELETE ON private_reference_image_operations FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_reference_media_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'legacy-reference-media-v1'::text$$;
COMMIT;
