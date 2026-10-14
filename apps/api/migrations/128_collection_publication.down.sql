BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM personal_collection_publication_operations) OR EXISTS(SELECT 1 FROM theme_maps WHERE visibility<>'private' OR status='published') THEN
  RAISE EXCEPTION 'collection publication history cannot be discarded by rollback' USING ERRCODE='23514';
 END IF;
END $$;
DROP TABLE personal_collection_publication_operations;
DROP FUNCTION birdtie_collection_publication_operation_immutable();
DROP INDEX theme_maps_public_city_time;
ALTER TABLE theme_maps DROP CONSTRAINT theme_map_public_confirmation;
ALTER TABLE theme_maps DROP COLUMN owner_confirmed_at;
ALTER TABLE theme_maps DROP COLUMN published_at;
ALTER TABLE theme_maps DROP CONSTRAINT theme_maps_visibility_check;
ALTER TABLE theme_maps ADD CONSTRAINT theme_maps_visibility_check CHECK(visibility='private');
ALTER TABLE theme_maps DROP CONSTRAINT theme_maps_status_check;
ALTER TABLE theme_maps ADD CONSTRAINT theme_maps_status_check CHECK(status IN('draft','withdrawn'));
COMMIT;
