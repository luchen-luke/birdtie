BEGIN;
-- Import provenance substitutes for an upload Session only for authenticated,
-- owner-bound legacy private derivatives. It never grants publication rights.
CREATE TABLE legacy_private_media_import_runs (
 run_id uuid PRIMARY KEY REFERENCES legacy_native_adaptation_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-private-images-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256 ~ '^[0-9a-f]{64}$'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256 ~ '^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256 ~ '^[0-9a-f]{64}$'),
 matcher_file_sha256 text NOT NULL CHECK(matcher_file_sha256 ~ '^[0-9a-f]{64}$'),
 authenticated_seal_sha256 text NOT NULL CHECK(authenticated_seal_sha256 ~ '^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'),
 deferred jsonb NOT NULL CHECK(jsonb_typeof(deferred)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_private_media_import_items (
 run_id uuid NOT NULL REFERENCES legacy_private_media_import_runs(run_id),
 source_note_id uuid NOT NULL,
 source_media_id uuid NOT NULL,
 source_media_sha256 text NOT NULL CHECK(source_media_sha256 ~ '^[0-9a-f]{64}$'),
 source_input_sha256 text NOT NULL CHECK(source_input_sha256 ~ '^[0-9a-f]{64}$'),
 source_input_bytes bigint NOT NULL CHECK(source_input_bytes BETWEEN 1 AND 16777216),
 source_position integer NOT NULL CHECK(source_position>=0),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 moment_id uuid NOT NULL REFERENCES moments(id),
 image_id uuid NOT NULL UNIQUE REFERENCES private_moment_images(id),
 derivative_sha256 text NOT NULL CHECK(derivative_sha256 ~ '^[0-9a-f]{64}$'),
 derivative_bytes bigint NOT NULL CHECK(derivative_bytes BETWEEN 1 AND 16777216),
 validation_kind text NOT NULL CHECK(validation_kind='STATIC_IMAGE_DECODE_REENCODE_V1'),
 antivirus_scan text NOT NULL CHECK(antivirus_scan='NOT_RUN'),
 privacy_risk text NOT NULL CHECK(privacy_risk='UNKNOWN'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(source_note_id,source_media_id)
);
CREATE INDEX legacy_private_media_owner ON legacy_private_media_import_items(owner_account_id,moment_id);
ALTER TABLE private_moment_images ALTER COLUMN session_id DROP NOT NULL;
ALTER TABLE private_moment_images DROP CONSTRAINT private_moment_images_input_size_check;
ALTER TABLE private_moment_images ADD CONSTRAINT private_moment_images_input_size_check CHECK(
 input_size>0 AND(input_size<=10485760 OR(session_id IS NULL AND input_size<=16777216)));

CREATE FUNCTION birdtie_legacy_private_media_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_native_adaptation_entities x
  JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key
   AND n.owner_account_id=x.owner_account_id AND n.source_sha256=x.source_sha256
  JOIN moments m ON m.id::text=x.target_id AND m.author_account_id=x.owner_account_id
  JOIN accounts a ON a.id=m.author_account_id
  JOIN private_moment_images i ON i.id=NEW.image_id AND i.owner_account_id=m.author_account_id AND i.moment_id=m.id
  JOIN legacy_private_media_import_runs r ON r.run_id=x.run_id
  JOIN media_assets asset ON asset.id=i.id AND asset.owner_account_id=i.owner_account_id
  WHERE x.run_id=NEW.run_id AND x.kind='moment' AND n.source_note_id=NEW.source_note_id AND NOT n.deleted
   AND EXISTS(SELECT 1 FROM jsonb_array_elements(n.source_relationships->'media') rel
    WHERE rel->>'sourceMediaId'=NEW.source_media_id::text AND rel->>'position'=NEW.source_position::text)
   AND m.id=NEW.moment_id AND m.author_account_id=NEW.owner_account_id AND m.status='draft' AND m.visibility='private'
   AND a.account_type='person' AND a.status IN('active','suspended')
   AND i.session_id IS NULL AND i.status='ready_private' AND i.mime_type='image/jpeg' AND i.pixel_risk='UNKNOWN'
   AND i.purpose='PRIVATE_MOMENT_ATTACHMENT' AND i.operation_id=i.id
   AND i.source_binding='legacy-private-import:'||r.projection_sha256 AND i.moment_revision=m.revision
   AND i.input_sha256=NEW.source_input_sha256 AND i.input_size=NEW.source_input_bytes
   AND i.derivative_sha256=NEW.derivative_sha256 AND octet_length(i.derivative)=NEW.derivative_bytes
   AND encode(sha256(i.derivative),'hex')=NEW.derivative_sha256
   AND asset.mime_type='image/jpeg' AND asset.byte_size=NEW.derivative_bytes
   AND asset.sha256_hex=NEW.derivative_sha256 AND asset.state='ready_private') THEN
  RAISE EXCEPTION 'original private native owner and verified derivative required';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_private_media_binding AFTER INSERT ON legacy_private_media_import_items
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_media_binding();

CREATE FUNCTION birdtie_private_image_import_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM private_moment_images i WHERE i.id=NEW.id AND i.session_id IS NULL
  AND NOT EXISTS(SELECT 1 FROM legacy_private_media_import_items x WHERE x.image_id=i.id
   AND x.owner_account_id=i.owner_account_id AND x.moment_id=i.moment_id
   AND x.source_input_sha256=i.input_sha256 AND x.source_input_bytes=i.input_size)) THEN
  RAISE EXCEPTION 'Session or immutable private import provenance required';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER private_image_import_provenance AFTER INSERT OR UPDATE ON private_moment_images
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_private_image_import_provenance();
CREATE FUNCTION birdtie_keep_private_import_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.session_id IS NULL THEN
  IF ROW(NEW.id,NEW.owner_account_id,NEW.moment_id,NEW.operation_id,NEW.moment_revision,
   NEW.session_id,NEW.mime_type,NEW.input_size,NEW.input_sha256,NEW.pixel_risk,NEW.purpose,
   NEW.created_at,NEW.preview_expires_at,NEW.retain_until)
   IS DISTINCT FROM ROW(OLD.id,OLD.owner_account_id,OLD.moment_id,OLD.operation_id,OLD.moment_revision,
   OLD.session_id,OLD.mime_type,OLD.input_size,OLD.input_sha256,OLD.pixel_risk,OLD.purpose,
   OLD.created_at,OLD.preview_expires_at,OLD.retain_until)
   OR (NEW.status<>'deleted' AND NEW.source_binding IS DISTINCT FROM OLD.source_binding)
   OR (OLD.status='deleted' AND NEW.status<>'deleted')
   OR (NEW.status<>'deleted' AND ROW(NEW.status,NEW.derivative,NEW.derivative_sha256)
    IS DISTINCT FROM ROW(OLD.status,OLD.derivative,OLD.derivative_sha256)) THEN
   RAISE EXCEPTION 'private import identity is immutable; deletion cannot resurrect bytes';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER private_image_import_identity BEFORE UPDATE ON private_moment_images
 FOR EACH ROW EXECUTE FUNCTION birdtie_keep_private_import_identity();
CREATE FUNCTION birdtie_legacy_private_media_counts() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual jsonb;
BEGIN
 SELECT jsonb_build_object('images',count(*),'notes',count(DISTINCT source_note_id),'owners',count(DISTINCT owner_account_id))
 INTO actual FROM legacy_private_media_import_items WHERE run_id=NEW.run_id;
 IF NEW.counts<>actual OR (actual->>'images')::bigint<1 THEN
  RAISE EXCEPTION 'private import counts must match immutable items';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_private_media_counts AFTER INSERT ON legacy_private_media_import_runs
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_media_counts();
CREATE TRIGGER legacy_private_media_run_immutable BEFORE UPDATE OR DELETE ON legacy_private_media_import_runs
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_private_media_item_immutable BEFORE UPDATE OR DELETE ON legacy_private_media_import_items
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
COMMIT;
