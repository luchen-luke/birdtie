BEGIN;
LOCK TABLE organization_map_location_audit IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM organization_map_location_audit WHERE request_id IS NOT NULL) THEN
  RAISE EXCEPTION 'preserve organization map request correlation history before downgrade';
 END IF;
END $$;
ALTER TABLE organization_map_location_audit DROP COLUMN request_id;
COMMIT;
