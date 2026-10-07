BEGIN;
-- Approval of 070 management facts is not public-disclosure consent.
-- No existing verified profile is backfilled or automatically published.
CREATE TABLE business_public_profile_permissions (
 business_id uuid PRIMARY KEY REFERENCES businesses(id),
 version bigint NOT NULL CHECK(version>0),
 profile_version bigint NOT NULL CHECK(profile_version>0),
 approved_by uuid NOT NULL REFERENCES accounts(id),
	 source_snapshot text NOT NULL CHECK(source_snapshot ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN ('active','revoked')),
 valid_until timestamptz NOT NULL CHECK(isfinite(valid_until)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(isfinite(created_at) AND valid_until>created_at)
);
CREATE TABLE business_public_profile_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 business_id uuid NOT NULL REFERENCES businesses(id),
 actor_account_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('publish','revoke')),
 permission_version bigint NOT NULL CHECK(permission_version>0),
 profile_version bigint NOT NULL CHECK(profile_version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(business_id,permission_version)
);
CREATE FUNCTION birdtie_public_profile_permission_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.state<>'active' THEN RAISE EXCEPTION 'First public permission must be explicit publication'; END IF;
 ELSE
  IF NEW.business_id<>OLD.business_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'Public permission requires stable subject and next revision';
  END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.approved_by AND account_type='person' AND status='active') THEN
  RAISE EXCEPTION 'Public permission needs an active human';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER business_public_permission_revision BEFORE INSERT OR UPDATE ON business_public_profile_permissions
 FOR EACH ROW EXECUTE FUNCTION birdtie_public_profile_permission_revision();
COMMIT;
