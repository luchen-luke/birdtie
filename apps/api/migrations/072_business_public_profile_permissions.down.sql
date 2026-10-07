BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM business_public_profile_permissions) OR EXISTS(SELECT 1 FROM business_public_profile_audit) THEN
  RAISE EXCEPTION 'Cannot remove public disclosure permissions or audit history';
 END IF;
END $$;
DROP TABLE business_public_profile_audit;
DROP TRIGGER business_public_permission_revision ON business_public_profile_permissions;
DROP FUNCTION birdtie_public_profile_permission_revision();
DROP TABLE business_public_profile_permissions;
COMMIT;
