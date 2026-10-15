BEGIN;
-- A new immutable stage, never an amendment to the 157/159 run receipts.
CREATE TABLE legacy_private_image_recovery_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_native_adaptation_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-private-image-recovery-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 matcher_file_sha256 text NOT NULL CHECK(matcher_file_sha256~'^[0-9a-f]{64}$'),
 authenticated_seal_sha256 text NOT NULL CHECK(authenticated_seal_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'),
 deferred jsonb NOT NULL CHECK(jsonb_typeof(deferred)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_private_image_recovery_items(
 run_id uuid NOT NULL REFERENCES legacy_private_image_recovery_runs(run_id),
 source_note_id uuid NOT NULL,source_media_id uuid NOT NULL,
 source_note_sha256 text NOT NULL CHECK(source_note_sha256~'^[0-9a-f]{64}$'),
 source_media_sha256 text NOT NULL CHECK(source_media_sha256~'^[0-9a-f]{64}$'),
 source_input_sha256 text NOT NULL CHECK(source_input_sha256~'^[0-9a-f]{64}$'),
 source_input_bytes bigint NOT NULL CHECK(source_input_bytes BETWEEN 1 AND 16777216),
 source_position integer NOT NULL CHECK(source_position>=0),
 source_mime text,source_declared_bytes bigint,
 recovery_reason text NOT NULL CHECK(recovery_reason IN('JPEG_UNKNOWN_SOURCE_METADATA','HEIC_MISCLASSIFIED_FTYP')),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),moment_id uuid NOT NULL REFERENCES moments(id),
 image_id uuid NOT NULL UNIQUE REFERENCES private_moment_images(id),
 derivative_sha256 text NOT NULL CHECK(derivative_sha256~'^[0-9a-f]{64}$'),
 derivative_bytes bigint NOT NULL CHECK(derivative_bytes BETWEEN 1 AND 16777216),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 8192),height integer NOT NULL CHECK(height BETWEEN 1 AND 8192),
 validation_kind text NOT NULL,
 antivirus_scan text NOT NULL CHECK(antivirus_scan='NOT_RUN'),privacy_risk text NOT NULL CHECK(privacy_risk='UNKNOWN'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(source_note_id,source_media_id),CHECK(width::bigint*height<=50331648),
 CHECK((recovery_reason='JPEG_UNKNOWN_SOURCE_METADATA' AND COALESCE(source_mime,'')='' AND(source_declared_bytes IS NULL OR source_declared_bytes=0) AND validation_kind='STATIC_IMAGE_DECODE_REENCODE_V1')
 OR(recovery_reason='HEIC_MISCLASSIFIED_FTYP' AND source_mime IS NOT NULL AND source_declared_bytes IS NOT NULL AND source_mime='image/heic' AND source_declared_bytes=source_input_bytes AND validation_kind='HEIC_FULL_DECODE_PNG_JPEG_REENCODE_V1'))
);
CREATE INDEX legacy_private_image_recovery_owner ON legacy_private_image_recovery_items(owner_account_id,moment_id,source_position);
CREATE FUNCTION birdtie_legacy_private_image_recovery_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM legacy_private_media_import_items previous_import WHERE previous_import.source_note_id=NEW.source_note_id AND previous_import.source_media_id=NEW.source_media_id)
 OR NOT EXISTS(SELECT 1 FROM legacy_private_image_recovery_runs r
 JOIN legacy_import_runs original ON original.run_id=r.run_id AND original.source_file_sha256=r.source_file_sha256 AND original.manifest_file_sha256=r.manifest_file_sha256
 JOIN legacy_native_adaptation_entities x ON x.run_id=r.run_id AND x.kind='moment' AND x.source_key=NEW.source_note_id::text AND x.target_id=NEW.moment_id::text AND x.owner_account_id=NEW.owner_account_id
 JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key AND n.owner_account_id=x.owner_account_id AND n.source_sha256=x.source_sha256 AND n.source_sha256=NEW.source_note_sha256 AND NOT n.deleted
 JOIN legacy_entity_mappings u ON u.run_id=n.run_id AND u.source_system=n.source_system AND u.entity_kind='user' AND u.target_id=n.owner_account_id AND u.owner_account_id=n.owner_account_id
 JOIN moments m ON m.id=NEW.moment_id AND m.author_account_id=NEW.owner_account_id
 JOIN accounts a ON a.id=m.author_account_id AND a.account_type='person' AND a.status IN('active','suspended')
 JOIN private_moment_images i ON i.id=NEW.image_id AND i.owner_account_id=m.author_account_id AND i.moment_id=m.id
 JOIN media_assets asset ON asset.id=i.id AND asset.owner_account_id=i.owner_account_id
 WHERE r.run_id=NEW.run_id AND m.status='draft' AND m.visibility='private'
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(n.source_relationships->'media') rel WHERE rel->>'sourceMediaId'=NEW.source_media_id::text AND rel->>'position'=NEW.source_position::text)
 AND i.session_id IS NULL AND i.status='ready_private' AND i.mime_type='image/jpeg' AND i.pixel_risk='UNKNOWN' AND i.purpose='PRIVATE_MOMENT_ATTACHMENT' AND i.operation_id=i.id
 AND i.source_binding='legacy-private-recovery:'||r.projection_sha256 AND i.moment_revision=m.revision
 AND i.input_sha256=NEW.source_input_sha256 AND i.input_size=NEW.source_input_bytes
 AND i.derivative_sha256=NEW.derivative_sha256 AND octet_length(i.derivative)=NEW.derivative_bytes AND encode(sha256(i.derivative),'hex')=NEW.derivative_sha256
 AND asset.state='ready_private' AND asset.mime_type='image/jpeg' AND asset.byte_size=NEW.derivative_bytes AND asset.sha256_hex=NEW.derivative_sha256)
 THEN RAISE EXCEPTION 'authenticated recovery and original private native owner required';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_private_image_recovery_binding AFTER INSERT ON legacy_private_image_recovery_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_image_recovery_binding();
CREATE OR REPLACE FUNCTION birdtie_private_image_import_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM private_moment_images i WHERE i.id=NEW.id AND i.session_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM legacy_private_media_import_items x WHERE x.image_id=i.id AND x.owner_account_id=i.owner_account_id AND x.moment_id=i.moment_id AND x.source_input_sha256=i.input_sha256 AND x.source_input_bytes=i.input_size)
 AND NOT EXISTS(SELECT 1 FROM legacy_private_image_recovery_items x WHERE x.image_id=i.id AND x.owner_account_id=i.owner_account_id AND x.moment_id=i.moment_id AND x.source_input_sha256=i.input_sha256 AND x.source_input_bytes=i.input_size))
 THEN RAISE EXCEPTION 'Session or immutable private import provenance required';END IF;
 RETURN NULL;
END $$;
CREATE FUNCTION birdtie_legacy_private_image_recovery_counts() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual jsonb;
BEGIN
 SELECT jsonb_build_object('images',count(*),'jpeg',count(*)FILTER(WHERE recovery_reason='JPEG_UNKNOWN_SOURCE_METADATA'),'heic',count(*)FILTER(WHERE recovery_reason='HEIC_MISCLASSIFIED_FTYP'),'notes',count(DISTINCT source_note_id),'owners',count(DISTINCT owner_account_id))INTO actual FROM legacy_private_image_recovery_items WHERE run_id=NEW.run_id;
 IF NEW.counts<>actual OR(actual->>'images')::bigint<1 THEN RAISE EXCEPTION 'recovery facts must match immutable items';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_private_image_recovery_counts AFTER INSERT ON legacy_private_image_recovery_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_image_recovery_counts();
CREATE TRIGGER legacy_private_image_recovery_run_immutable BEFORE UPDATE OR DELETE ON legacy_private_image_recovery_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_private_image_recovery_item_immutable BEFORE UPDATE OR DELETE ON legacy_private_image_recovery_items FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_private_image_recovery_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'legacy-private-image-recovery-v1'::text$$;
COMMIT;
