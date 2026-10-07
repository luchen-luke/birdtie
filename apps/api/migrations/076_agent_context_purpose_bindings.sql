BEGIN;

-- consent_grants remains the ONLY revision / expiry / revocation authority.
-- No old profile/view/relationship setting becomes a Task cognition grant.
CREATE TABLE agent_context_purpose_previews (
 id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'::uuid),
 owner_id uuid NOT NULL REFERENCES accounts(id),
 agent_id uuid NOT NULL,
 owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 session_id uuid NOT NULL REFERENCES sessions(id),
 task_id uuid NOT NULL REFERENCES agent_tasks(id),
 selection jsonb NOT NULL CHECK(jsonb_typeof(selection)='object' AND octet_length(selection::text)<=8192 AND NOT selection?'currentQuery'),
 sources jsonb NOT NULL CHECK(jsonb_typeof(sources)='array' AND jsonb_array_length(sources) BETWEEN 2 AND 20 AND octet_length(sources::text)<=16384),
 authority text NOT NULL CHECK(authority ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL CHECK(isfinite(observed_at)),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type),
 CHECK(expires_at>observed_at AND expires_at<=observed_at+interval '5 minutes')
);
CREATE INDEX agent_context_purpose_previews_owner ON agent_context_purpose_previews(owner_id,observed_at);

-- A binding has no independent state, revision, expiry or revocation. The
-- immutable original preview supplies the exact selected sources and identity.
-- Its UNIQUE preview_id is the sole once-consumption key, with no shadow ledger.
CREATE TABLE agent_context_purpose_bindings (
 grant_id uuid PRIMARY KEY REFERENCES consent_grants(id),
 preview_id uuid NOT NULL UNIQUE REFERENCES agent_context_purpose_previews(id)
);

CREATE FUNCTION birdtie_context_purpose_preview_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'context purpose preview is immutable' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
  JOIN sessions se ON se.account_id=a.id JOIN agent_tasks t ON t.owner_account_id=a.id
  WHERE a.id=NEW.owner_id AND a.account_type='person' AND a.status='active'
   AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active'
   AND se.id=NEW.session_id AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()
   AND t.id=NEW.task_id AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.status='ACTIVE') THEN
  RAISE EXCEPTION 'context purpose identity unavailable' USING ERRCODE='23514';END IF;
 IF NEW.selection->>'agentId' IS DISTINCT FROM NEW.agent_id::text OR NEW.selection->>'taskId' IS DISTINCT FROM NEW.task_id::text
  OR NEW.selection->>'queryDigest' !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'context purpose selection binding invalid' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_context_purpose_preview_guard BEFORE INSERT OR UPDATE ON agent_context_purpose_previews
 FOR EACH ROW EXECUTE FUNCTION birdtie_context_purpose_preview_guard();
CREATE FUNCTION birdtie_context_purpose_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'context purpose binding is immutable' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_context_purpose_previews p ON p.id=NEW.preview_id
  WHERE g.id=NEW.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id
   AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='TASK_CONTEXT_READ'
   AND g.actions=ARRAY['read']::text[] AND g.revision=1 AND g.revoked_at IS NULL
   AND g.expires_at IS NOT NULL AND g.expires_at>clock_timestamp()
   AND p.expires_at>clock_timestamp()) THEN
  RAISE EXCEPTION 'context purpose grant binding invalid' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_context_purpose_binding_guard BEFORE INSERT OR UPDATE ON agent_context_purpose_bindings
 FOR EACH ROW EXECUTE FUNCTION birdtie_context_purpose_binding_guard();

-- Applies only to this exact new purpose. Existing consent semantics and old
-- records are untouched. A grant cannot be upgraded, renewed or un-revoked.
CREATE FUNCTION birdtie_context_purpose_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.purpose<>'TASK_CONTEXT_READ' AND NEW.purpose<>'TASK_CONTEXT_READ' THEN RETURN NEW;END IF;
 IF TG_OP='INSERT' AND NEW.purpose<>'TASK_CONTEXT_READ' THEN RETURN NEW;END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.revision<>1 OR NEW.revoked_at IS NOT NULL OR NEW.resource_type<>'agent_context'
   OR NEW.recipient_account_id IS DISTINCT FROM NEW.owner_account_id OR NEW.actions<>ARRAY['read']::text[]
   OR NEW.expires_at IS NULL OR NOT isfinite(NEW.created_at) OR NOT isfinite(NEW.expires_at)
   OR NEW.expires_at<=clock_timestamp() OR NEW.created_at>clock_timestamp()
   OR NEW.expires_at>NEW.created_at+interval '15 minutes'
   OR NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews p WHERE p.id::text=NEW.resource_id
    AND p.owner_id=NEW.owner_account_id AND p.expires_at>clock_timestamp()) THEN
   RAISE EXCEPTION 'context purpose initial grant invalid' USING ERRCODE='23514';END IF;
 ELSE
  IF OLD.purpose<>'TASK_CONTEXT_READ' OR NEW.id IS DISTINCT FROM OLD.id
   OR NEW.owner_account_id IS DISTINCT FROM OLD.owner_account_id OR NEW.recipient_account_id IS DISTINCT FROM OLD.recipient_account_id
   OR NEW.resource_type IS DISTINCT FROM OLD.resource_type OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
   OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.actions IS DISTINCT FROM OLD.actions
   OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
   OR OLD.revoked_at IS NOT NULL OR OLD.revision=9223372036854775807 OR NEW.revision<>OLD.revision+1
   OR NEW.revoked_at IS NULL OR NOT isfinite(NEW.revoked_at) OR NEW.revoked_at<OLD.created_at OR NEW.revoked_at>clock_timestamp() THEN
   RAISE EXCEPTION 'context purpose grant permits only irreversible revisioned revoke' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_context_purpose_grant_guard BEFORE INSERT OR UPDATE ON consent_grants
 FOR EACH ROW EXECUTE FUNCTION birdtie_context_purpose_grant_guard();

COMMIT;
