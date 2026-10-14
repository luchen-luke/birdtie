BEGIN;
-- Native device transport only: OFF until trusted runtime configuration and an
-- explicit current-person registration. No Agent execution or historical replay.
CREATE TABLE system_push_installations (
 installation_id uuid PRIMARY KEY,
 proof_sha256 bytea NOT NULL CHECK(octet_length(proof_sha256)=32),
 version bigint NOT NULL DEFAULT 0 CHECK(version BETWEEN 0 AND 4503599627370496),
 owner_id uuid REFERENCES accounts(id),
 -- Captured grant identifier is retained for AEAD AAD after Session deletion.
 -- A missing original Session always fails the current binding fence.
 session_id uuid,
 session_generation bigint,
 binding_id uuid,
 platform text,
 registration_kind text CHECK(registration_kind IN ('FCM_TOKEN','APNS_TOKEN')),
 application_id text,
 environment text,
 permission text,
 state text NOT NULL DEFAULT 'REVOKED' CHECK(state IN ('ACTIVE','REVOKED')),
 address_key_id text,
 address_nonce bytea,
 address_ciphertext bytea,
 address_fingerprint bytea,
 registered_at timestamptz,
 valid_until timestamptz,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(state='REVOKED' OR (version>0 AND owner_id IS NOT NULL AND session_id IS NOT NULL AND session_generation IS NOT NULL AND session_generation>0 AND binding_id IS NOT NULL
  AND registration_kind IS NOT NULL AND platform IS NOT NULL AND permission IS NOT NULL AND environment IS NOT NULL
  AND ((registration_kind='FCM_TOKEN' AND platform='ANDROID' AND permission='AUTHORIZED' AND environment='FCM')
    OR (registration_kind='APNS_TOKEN' AND platform='IOS' AND permission IN ('AUTHORIZED','PROVISIONAL') AND environment IN ('sandbox','production')))
  AND application_id IS NOT NULL AND address_key_id IS NOT NULL AND address_nonce IS NOT NULL AND address_ciphertext IS NOT NULL AND address_fingerprint IS NOT NULL AND registered_at IS NOT NULL AND valid_until IS NOT NULL AND address_key_id~'^[a-zA-Z0-9_-]{1,64}$'
  AND octet_length(address_nonce)=12 AND octet_length(address_ciphertext) BETWEEN 48 AND 4112
  AND octet_length(address_fingerprint)=32 AND isfinite(registered_at) AND isfinite(valid_until)
  AND valid_until>registered_at AND valid_until<=registered_at+interval '30 days')),
 CHECK(state='ACTIVE' OR (binding_id IS NULL AND address_key_id IS NULL AND address_nonce IS NULL AND address_ciphertext IS NULL AND address_fingerprint IS NULL))
);
CREATE UNIQUE INDEX system_push_active_address ON system_push_installations(registration_kind,application_id,environment,address_fingerprint) WHERE state='ACTIVE';
CREATE INDEX system_push_owner ON system_push_installations(owner_id) WHERE state='ACTIVE';
CREATE TABLE system_push_operations (
 installation_id uuid NOT NULL REFERENCES system_push_installations(installation_id),
 operation_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES accounts(id),
 request_digest bytea NOT NULL CHECK(octet_length(request_digest)=32),
 operation text NOT NULL CHECK(operation IN ('REGISTER','REVOKE')),
 expected_version bigint NOT NULL CHECK(expected_version>=0),
 result_version bigint NOT NULL CHECK(result_version=expected_version+1 AND result_version<=4503599627370496),
 binding_id uuid,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((operation='REGISTER' AND binding_id IS NOT NULL) OR (operation='REVOKE' AND binding_id IS NULL)),
 PRIMARY KEY(installation_id,operation_id)
);
CREATE FUNCTION birdtie_system_push_operation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'system push operation is immutable' USING ERRCODE='23514';END $$;
CREATE TRIGGER system_push_operation_immutable BEFORE UPDATE ON system_push_operations FOR EACH ROW EXECUTE FUNCTION birdtie_system_push_operation_immutable();
CREATE TABLE system_push_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 inbox_id uuid REFERENCES inbox_items(id) ON DELETE SET NULL,
 recipient_id uuid NOT NULL REFERENCES accounts(id),
 installation_id uuid NOT NULL REFERENCES system_push_installations(installation_id),
 binding_id uuid NOT NULL,
 binding_version bigint NOT NULL CHECK(binding_version>0),
 registration_kind text NOT NULL CHECK(registration_kind IN ('FCM_TOKEN','APNS_TOKEN')),
 state text NOT NULL DEFAULT 'QUEUED' CHECK(state IN ('QUEUED','PREPARED','SENDING','RETRY_WAIT','ACCEPTED','REJECTED','UNKNOWN','CANCELLED')),
 expires_at timestamptz NOT NULL,
 lease_until timestamptz,
 current_attempt_id uuid,
 attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count BETWEEN 0 AND 5),
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(inbox_id,installation_id,binding_id,binding_version)
);
CREATE INDEX system_push_due ON system_push_events(next_attempt_at,id) WHERE state IN ('QUEUED','RETRY_WAIT','PREPARED','SENDING');
CREATE TABLE system_push_attempts (
 id uuid PRIMARY KEY,
 event_id uuid NOT NULL REFERENCES system_push_events(id),
 state text NOT NULL CHECK(state IN ('PREPARED','SENDING','ACCEPTED','RETRY','REJECTED','INVALID_TOKEN','UNKNOWN')),
 code text,
 provider_message_id text,
 invalidated_at timestamptz,
 retry_at timestamptz,
 prepared_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 started_at timestamptz,
 finished_at timestamptz,
 CHECK(code IS NULL OR code~'^[A-Z_]{1,64}$'),
 CHECK(provider_message_id IS NULL OR length(provider_message_id)<=256)
);
CREATE FUNCTION birdtie_system_push_attempt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id<>OLD.id OR NEW.event_id<>OLD.event_id OR NEW.prepared_at<>OLD.prepared_at
 OR OLD.state NOT IN ('PREPARED','SENDING') OR (OLD.state='SENDING' AND NEW.state='PREPARED')
 THEN RAISE EXCEPTION 'system push attempt is immutable' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER system_push_attempt_guard BEFORE UPDATE ON system_push_attempts FOR EACH ROW EXECUTE FUNCTION birdtie_system_push_attempt_guard();
CREATE FUNCTION birdtie_system_push_enqueue() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO system_push_events(inbox_id,recipient_id,installation_id,binding_id,binding_version,registration_kind,expires_at)
 SELECT NEW.id,NEW.recipient_account_id,p.installation_id,p.binding_id,p.version,p.registration_kind,
 LEAST(p.valid_until,clock_timestamp()+interval '24 hours')
 FROM system_push_installations p JOIN sessions ss ON ss.id=p.session_id AND ss.account_id=p.owner_id
 JOIN accounts a ON a.id=p.owner_id
 JOIN native_notification_decisions d ON d.id=NEW.routing_decision_id AND d.recipient_id=p.owner_id
 WHERE p.owner_id=NEW.recipient_account_id AND p.state='ACTIVE' AND p.valid_until>clock_timestamp()
 AND a.account_type='person' AND a.status='active' AND ss.authorization_generation=p.session_generation
 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp()
 AND NEW.read_at IS NULL AND d.disposition IN ('IMMEDIATE','NORMAL') AND birdtie_native_notification_visible(d)
 ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER system_push_enqueue AFTER INSERT ON inbox_items FOR EACH ROW EXECUTE FUNCTION birdtie_system_push_enqueue();
COMMIT;
