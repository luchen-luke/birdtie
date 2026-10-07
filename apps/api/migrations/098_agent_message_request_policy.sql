BEGIN;
-- Receiver preferences only. Existing requests/acceptance remain contact authority.
CREATE TABLE agent_message_request_policies(
 agent_id uuid PRIMARY KEY,owner_id uuid NOT NULL UNIQUE,owner_type text NOT NULL CHECK(owner_type='PERSON'),
 native_revision bigint NOT NULL CHECK(native_revision>0),incoming_requests text NOT NULL CHECK(incoming_requests IN('REQUEST','SCREEN','BLOCK')),
 valid_from timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
 CHECK(isfinite(valid_from) AND isfinite(expires_at) AND expires_at>valid_from AND expires_at<=valid_from+interval '30 days')
);
CREATE FUNCTION birdtie_guard_message_request_policy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM agents ag JOIN accounts a ON a.id=ag.principal_account_id WHERE ag.id=NEW.agent_id AND a.id=NEW.owner_id AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND ag.status='active') THEN RAISE EXCEPTION 'message policy requires active exact Person Agent' USING ERRCODE='23514';END IF;
 IF TG_OP='INSERT' THEN IF NEW.native_revision<>1 THEN RAISE EXCEPTION 'initial message policy revision must be one' USING ERRCODE='23514';END IF;
 ELSE IF NEW.agent_id<>OLD.agent_id OR NEW.owner_id<>OLD.owner_id OR NEW.owner_type<>OLD.owner_type OR NEW.native_revision<>OLD.native_revision+1 THEN RAISE EXCEPTION 'immutable message policy binding and CAS required' USING ERRCODE='23514';END IF;END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_message_request_policy_guard BEFORE INSERT OR UPDATE ON agent_message_request_policies FOR EACH ROW EXECUTE FUNCTION birdtie_guard_message_request_policy();
-- One immutable routing annotation for the SAME original Request ID, no grant.
CREATE TABLE connection_request_policy_bindings(
 request_id uuid PRIMARY KEY REFERENCES connection_requests(id) ON DELETE CASCADE,
 recipient_id uuid NOT NULL REFERENCES accounts(id),disposition text NOT NULL CHECK(disposition IN('REQUEST','SCREEN')),
 policy_version text NOT NULL CHECK(policy_version~'^[0-9a-f]{64}$'),observed_at timestamptz NOT NULL CHECK(isfinite(observed_at))
);
CREATE FUNCTION birdtie_guard_message_request_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'immutable request routing annotation' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM connection_requests r WHERE r.id=NEW.request_id AND r.recipient_account_id=NEW.recipient_id AND r.state='pending') THEN RAISE EXCEPTION 'routing requires original pending Request' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER connection_request_policy_binding_guard BEFORE INSERT OR UPDATE ON connection_request_policy_bindings FOR EACH ROW EXECUTE FUNCTION birdtie_guard_message_request_binding();
COMMIT;
