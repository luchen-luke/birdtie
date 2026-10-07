BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM activities WHERE venue_place_id IS NOT NULL OR
        modality IN ('online','hybrid') OR physical_place_status='tbd' OR
        (modality='unspecified' AND place_id IS NOT NULL) OR
        (modality='in_person' AND physical_place_status<>'confirmed')) THEN
        RAISE EXCEPTION 'cannot remove explicit Activity location semantics while sourced data exists';
    END IF;
END $$;
DROP TRIGGER activity_place_venue_guard ON activities;
DROP FUNCTION birdtie_activity_place_venue_guard();
ALTER TABLE activities DROP CONSTRAINT activity_physical_place_consistent;
ALTER TABLE activities DROP COLUMN venue_place_id;
ALTER TABLE activities DROP COLUMN physical_place_status;
ALTER TABLE activities DROP COLUMN modality;
COMMIT;
