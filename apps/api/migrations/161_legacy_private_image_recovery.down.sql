BEGIN;
DO $$BEGIN IF EXISTS(SELECT 1 FROM legacy_private_image_recovery_runs) OR EXISTS(SELECT 1 FROM legacy_private_image_recovery_items) THEN RAISE EXCEPTION 'recovery provenance cannot be discarded';END IF;END$$;
CREATE OR REPLACE FUNCTION birdtie_private_image_import_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM private_moment_images i WHERE i.id=NEW.id AND i.session_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM legacy_private_media_import_items x WHERE x.image_id=i.id AND x.owner_account_id=i.owner_account_id AND x.moment_id=i.moment_id AND x.source_input_sha256=i.input_sha256 AND x.source_input_bytes=i.input_size))THEN RAISE EXCEPTION 'Session or immutable private import provenance required';END IF;
 RETURN NULL;
END$$;
DROP TABLE legacy_private_image_recovery_items;
DROP TABLE legacy_private_image_recovery_runs;
DROP FUNCTION birdtie_legacy_private_image_recovery_binding();
DROP FUNCTION birdtie_legacy_private_image_recovery_counts();
DROP FUNCTION birdtie_legacy_private_image_recovery_contract();
COMMIT;
