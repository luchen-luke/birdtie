BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM legacy_private_media_import_runs) OR EXISTS(SELECT 1 FROM private_moment_images WHERE session_id IS NULL) THEN
  RAISE EXCEPTION 'preserve private import provenance; populated migration cannot be downgraded';
 END IF;
END $$;
DROP TRIGGER private_image_import_identity ON private_moment_images;
DROP TRIGGER private_image_import_provenance ON private_moment_images;
DROP FUNCTION birdtie_keep_private_import_identity();
DROP FUNCTION birdtie_private_image_import_provenance();
DROP TABLE legacy_private_media_import_items;
DROP TABLE legacy_private_media_import_runs;
DROP FUNCTION birdtie_legacy_private_media_binding();
DROP FUNCTION birdtie_legacy_private_media_counts();
ALTER TABLE private_moment_images DROP CONSTRAINT private_moment_images_input_size_check;
ALTER TABLE private_moment_images ADD CONSTRAINT private_moment_images_input_size_check CHECK(input_size>0 AND input_size<=10485760);
ALTER TABLE private_moment_images ALTER COLUMN session_id SET NOT NULL;
COMMIT;
