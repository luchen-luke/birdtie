BEGIN;

-- A device credential can issue a fresh, ordinary Session. It grants no domain
-- permission and never revives an expired Session or its previous approvals.
CREATE TABLE persistent_session_families (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id uuid NOT NULL REFERENCES accounts(id),
 identity_issuer text NOT NULL,
 identity_subject text NOT NULL,
 authentication_method text NOT NULL CHECK(authentication_method='dev_phone'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 FOREIGN KEY(identity_issuer,identity_subject) REFERENCES account_auth_identities(issuer,subject),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 days')
);
CREATE TABLE persistent_session_tokens (
 token_sha256 bytea PRIMARY KEY CHECK(octet_length(token_sha256)=32),
 family_id uuid NOT NULL REFERENCES persistent_session_families(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 consumed_at timestamptz,
 operation_id uuid,
 successor_sha256 bytea,
 receipt_ciphertext bytea,
 receipt_expires_at timestamptz,
 UNIQUE(family_id,token_sha256),
 FOREIGN KEY(family_id,successor_sha256) REFERENCES persistent_session_tokens(family_id,token_sha256) DEFERRABLE INITIALLY DEFERRED,
 CHECK((consumed_at IS NULL AND operation_id IS NULL AND successor_sha256 IS NULL AND receipt_ciphertext IS NULL AND receipt_expires_at IS NULL)
    OR(consumed_at IS NOT NULL AND operation_id IS NOT NULL AND successor_sha256 IS NOT NULL
       AND receipt_ciphertext IS NOT NULL AND octet_length(receipt_ciphertext) BETWEEN 32 AND 4096
       AND receipt_expires_at>consumed_at AND receipt_expires_at<=consumed_at+interval '60 seconds'))
);
CREATE UNIQUE INDEX persistent_session_one_current_token ON persistent_session_tokens(family_id) WHERE consumed_at IS NULL;
CREATE TABLE persistent_session_access (
 session_id uuid PRIMARY KEY REFERENCES sessions(id),
 family_id uuid NOT NULL REFERENCES persistent_session_families(id)
);
CREATE INDEX persistent_session_access_family ON persistent_session_access(family_id);

CREATE FUNCTION birdtie_persistent_session_family_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'device family history retained' USING ERRCODE='23514'; END IF;
 IF (to_jsonb(NEW)-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'revoked_at')
    OR(OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
    OR(OLD.revoked_at IS NULL AND NEW.revoked_at IS NULL) THEN
  RAISE EXCEPTION 'device family identity and deadline immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER persistent_session_family_guard BEFORE UPDATE OR DELETE ON persistent_session_families
 FOR EACH ROW EXECUTE FUNCTION birdtie_persistent_session_family_guard();

COMMIT;
