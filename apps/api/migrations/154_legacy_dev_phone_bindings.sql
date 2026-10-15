-- Exact preserved owners may enroll only through the protected fixed-code test
-- channel. This is not SMS verification, a production claim, or Agent activation.
CREATE TABLE legacy_dev_phone_bindings (
 phone_digest bytea PRIMARY KEY CHECK(octet_length(phone_digest)=32),
 source_system text NOT NULL CHECK(source_system='hangzhou-civu-20261014'),
 source_user_id uuid NOT NULL,owner_account_id uuid NOT NULL REFERENCES accounts(id),
 run_id uuid NOT NULL REFERENCES legacy_import_runs(run_id),
 source_owner_version bigint NOT NULL CHECK(source_owner_version BETWEEN 1 AND 8999999999999999),
 source_owner_sha256 text NOT NULL CHECK(source_owner_sha256 ~ '^[0-9a-f]{64}$'),
 source_identity_sha256 text NOT NULL CHECK(source_identity_sha256 ~ '^[0-9a-f]{64}$'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256 ~ '^[0-9a-f]{64}$'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256 ~ '^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256 ~ '^[0-9a-f]{64}$'),
 initial_account_stamp text NOT NULL CHECK(initial_account_stamp ~ '^[0-9]+$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 first_enrolled_at timestamptz,
 UNIQUE(source_system,source_user_id),UNIQUE(owner_account_id),
 CHECK(first_enrolled_at IS NULL OR first_enrolled_at>=created_at)
);
CREATE INDEX legacy_dev_phone_binding_run ON legacy_dev_phone_bindings(run_id);

CREATE FUNCTION birdtie_guard_legacy_dev_phone_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original test phone binding is immutable';END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.first_enrolled_at IS NOT NULL OR NEW.first_enrolled_at IS NULL
   OR (to_jsonb(NEW)-'first_enrolled_at') IS DISTINCT FROM (to_jsonb(OLD)-'first_enrolled_at')
   OR NEW.first_enrolled_at>clock_timestamp() THEN
   RAISE EXCEPTION 'only original first test enrollment is allowed';
  END IF;
 ELSE
  IF NEW.first_enrolled_at IS NOT NULL THEN RAISE EXCEPTION 'test binding must start unenrolled';END IF;
 END IF;
 IF NOT EXISTS(
  SELECT 1 FROM legacy_entity_mappings m JOIN legacy_import_runs r ON r.run_id=m.run_id
  JOIN accounts a ON a.id=m.owner_account_id JOIN user_profiles p ON p.account_id=a.id
  JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal'
  WHERE m.source_system=NEW.source_system AND m.entity_kind='user' AND m.source_id=NEW.source_user_id
   AND m.target_id=NEW.owner_account_id AND m.owner_account_id=NEW.owner_account_id
   AND m.run_id=NEW.run_id AND m.source_version=NEW.source_owner_version
   AND m.source_sha256=NEW.source_owner_sha256 AND m.source_status='active' AND m.claim_status='UNCLAIMED'
   AND r.snapshot_sha256=NEW.snapshot_sha256 AND r.source_file_sha256=NEW.source_file_sha256
   AND r.manifest_file_sha256=NEW.manifest_file_sha256 AND a.account_type='person'
   AND p.visibility='private' AND ag.status='suspended'
   AND CASE WHEN TG_OP='INSERT' THEN
    a.xmin::text=NEW.initial_account_stamp AND(a.status='suspended' OR(a.status='active' AND EXISTS(
     SELECT 1 FROM account_auth_identities ai WHERE ai.issuer='urn:birdtie:local-dev-phone'
      AND ai.subject=encode(NEW.phone_digest,'hex') AND ai.account_id=a.id)))
   ELSE a.status='active' AND EXISTS(
    SELECT 1 FROM account_auth_identities ai WHERE ai.issuer='urn:birdtie:local-dev-phone'
     AND ai.subject=encode(NEW.phone_digest,'hex') AND ai.account_id=a.id)
   END
 ) THEN RAISE EXCEPTION 'exact private preserved owner and paused Agent required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_dev_phone_binding_guard BEFORE INSERT OR UPDATE OR DELETE ON legacy_dev_phone_bindings
 FOR EACH ROW EXECUTE FUNCTION birdtie_guard_legacy_dev_phone_binding();
CREATE FUNCTION birdtie_legacy_dev_phone_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT 'legacy-dev-phone-binding-v1'::text
$$;
COMMENT ON TABLE legacy_dev_phone_bindings IS 'Only fixed-code protected dev login; canonical phone digest, not raw phone/identity/credential. Immutable 146 UNCLAIMED history remains; first enrollment activates only Account, never Agent. No SMS or production ownership proof.';
