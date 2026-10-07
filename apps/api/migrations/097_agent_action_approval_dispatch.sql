BEGIN;
-- The existing consent_grants remains the sole grant lifecycle. This closed
-- purpose does not authorize model export, memory, messages or organization acts.
CREATE TABLE agent_action_approvals(
 id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES accounts(id),
 agent_id uuid NOT NULL, owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 task_id uuid NOT NULL REFERENCES agent_tasks(id), session_id uuid NOT NULL REFERENCES sessions(id),
 grant_id uuid NOT NULL UNIQUE,
 binding_canonical text NOT NULL CHECK(octet_length(binding_canonical)<=8192),
 binding_digest text NOT NULL CHECK(binding_digest~'^[0-9a-f]{64}$'),
 payload_canonical text NOT NULL CHECK(octet_length(payload_canonical)<=2048),
 observed_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 approved_at timestamptz, consumed_at timestamptz, cancelled_at timestamptz,
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type),
 CHECK(isfinite(observed_at) AND isfinite(expires_at) AND expires_at>observed_at AND expires_at<=observed_at+interval '30 seconds')
);
CREATE TABLE agent_action_dispatches(
 id uuid PRIMARY KEY, approval_id uuid NOT NULL UNIQUE REFERENCES agent_action_approvals(id),
 owner_id uuid NOT NULL REFERENCES accounts(id), effect_key text NOT NULL CHECK(effect_key~'^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN('DISPATCH_COMMITTED','IN_FLIGHT','UNKNOWN_OUTCOME','SUCCEEDED','NO_EFFECT')),
 committed_at timestamptz NOT NULL, started_at timestamptz, lease_until timestamptz,
 fence bigint NOT NULL DEFAULT 0 CHECK(fence>=0), effect_id uuid, applied_at timestamptz,
 UNIQUE(owner_id,effect_key),CHECK((effect_id IS NULL)=(applied_at IS NULL)),
 CHECK((state='SUCCEEDED')=(effect_id IS NOT NULL))
);
CREATE TABLE agent_sandbox_writes(
 id uuid PRIMARY KEY, dispatch_id uuid NOT NULL UNIQUE REFERENCES agent_action_dispatches(id),
 owner_id uuid NOT NULL REFERENCES accounts(id), target_id uuid NOT NULL REFERENCES accounts(id),
 effect_key text NOT NULL, value text NOT NULL CHECK(octet_length(value) BETWEEN 1 AND 240),
 applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(owner_id,effect_key),CHECK(owner_id=target_id)
);
CREATE FUNCTION birdtie_sandbox_action_authority(sid uuid,oid uuid,aid uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('account',to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text),
 'agent',to_jsonb(ag)||jsonb_build_object('xmin',ag.xmin::text),'profile',to_jsonb(ap)||jsonb_build_object('xmin',ap.xmin::text),
 'session',jsonb_build_object('id',s.id,'account',s.account_id,'expires',s.expires_at,'idle',s.idle_expires_at,'revoked',s.revoked_at,'method',s.authentication_method,'xmin',s.xmin::text))::text,'UTF8')),'hex')
 FROM sessions s JOIN accounts a ON a.id=s.account_id JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE s.id=sid AND a.id=oid AND ag.id=aid $$;
CREATE FUNCTION birdtie_sandbox_action_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM agent_action_approvals p JOIN sessions s ON s.id=p.session_id JOIN accounts a ON a.id=p.owner_id
 JOIN agents ag ON ag.id=p.agent_id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN agent_tasks t ON t.id=p.task_id CROSS JOIN LATERAL(SELECT p.binding_canonical::jsonb b) x
 WHERE p.id=pid AND s.account_id=a.id AND a.account_type='person' AND a.status='active' AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 AND t.owner_account_id=a.id AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.status='ACTIVE' AND t.xmin::text=x.b->>'source_generation'
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=a.id AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id)
 AND birdtie_sandbox_action_authority(s.id,a.id,ag.id)=x.b->>'authority_version'
 AND (SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(ps)||jsonb_build_object('_xmin',ps.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings ps WHERE ps.owner_id=a.id AND ps.agent_id=ag.id AND ps.owner_type='PERSON')=x.b->>'policy_version'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp() AND p.expires_at>clock_timestamp() AND p.cancelled_at IS NULL) $$;
CREATE FUNCTION birdtie_sandbox_action_approval_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE b jsonb;v jsonb;
BEGIN
 b:=NEW.binding_canonical::jsonb;v:=NEW.payload_canonical::jsonb;
 IF TG_OP='UPDATE' THEN
  IF (to_jsonb(NEW)-ARRAY['approved_at','consumed_at','cancelled_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['approved_at','consumed_at','cancelled_at'])
   OR (OLD.approved_at IS NOT NULL AND NEW.approved_at IS DISTINCT FROM OLD.approved_at)
   OR (OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at)
   OR (OLD.cancelled_at IS NOT NULL AND NEW.cancelled_at IS DISTINCT FROM OLD.cancelled_at)
   OR (NEW.consumed_at IS NOT NULL AND NEW.approved_at IS NULL) THEN RAISE EXCEPTION 'immutable sandbox approval' USING ERRCODE='23514';END IF;
 ELSE
  IF NEW.approved_at IS NOT NULL OR NEW.consumed_at IS NOT NULL OR NEW.cancelled_at IS NOT NULL THEN RAISE EXCEPTION 'new preview is not approved' USING ERRCODE='23514';END IF;
 END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(b))<>25 OR (SELECT count(*) FROM jsonb_object_keys(v))<>5
 OR NOT(b ?& ARRAY['schema_version','approval_id','tenant_id','actor_id','subject_type','subject_id','agent_id','session_id','task_id','logical_operation_id','action_id','tool','tool_version','target_id','payload_digest','source_version','source_generation','authority_version','policy_version','consent_purpose','grant_id','grant_revision','membership_version','observed_at','expires_at'])
 OR NOT(v ?& ARRAY['action_id','logical_operation_id','target_id','value','resource_version'])
 OR b->>'schema_version'<>'air.sandbox_action.v1' OR b->>'approval_id'<>NEW.id::text OR b->>'tenant_id'<>NEW.owner_id::text OR b->>'actor_id'<>NEW.owner_id::text OR b->>'subject_type'<>'PERSON' OR b->>'subject_id'<>NEW.owner_id::text OR b->>'agent_id'<>NEW.agent_id::text OR b->>'session_id'<>NEW.session_id::text OR b->>'task_id'<>NEW.task_id::text
 OR b->>'target_id'<>NEW.owner_id::text OR b->>'tool'<>'sandbox.write' OR b->>'tool_version'<>'sandbox.write.v1' OR b->>'consent_purpose'<>'OWN_SANDBOX_ACTION' OR b->>'grant_id'<>NEW.grant_id::text OR (b->>'grant_revision')::bigint<>1 OR b->>'membership_version'<>'NONE_PERSON_ONLY'
 OR (b->>'observed_at')::timestamptz<>NEW.observed_at OR (b->>'expires_at')::timestamptz<>NEW.expires_at
 OR b->>'action_id'<>v->>'action_id' OR b->>'logical_operation_id'<>v->>'logical_operation_id' OR b->>'target_id'<>v->>'target_id' OR b->>'source_version'<>v->>'resource_version'
 OR b->>'payload_digest'<>encode(sha256(convert_to('birdtie.native-tool-metadata.v1','UTF8')||decode('00','hex')||convert_to(NEW.payload_canonical,'UTF8')),'hex')
 OR NEW.binding_digest<>encode(sha256(convert_to('birdtie.sandbox-approval.v1:'||NEW.binding_canonical,'UTF8')),'hex')
 THEN RAISE EXCEPTION 'invalid sandbox binding' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_action_approval_guard BEFORE INSERT OR UPDATE ON agent_action_approvals FOR EACH ROW EXECUTE FUNCTION birdtie_sandbox_action_approval_guard();
CREATE FUNCTION birdtie_sandbox_action_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND NEW.purpose<>'OWN_SANDBOX_ACTION' THEN RETURN NEW;END IF;
 IF TG_OP='UPDATE' AND OLD.purpose<>'OWN_SANDBOX_ACTION' AND NEW.purpose<>'OWN_SANDBOX_ACTION' THEN RETURN NEW;END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.resource_type<>'agent_context' OR NEW.owner_account_id IS DISTINCT FROM NEW.recipient_account_id OR NEW.actions<>ARRAY['sandbox_write']::text[] OR NEW.revision<>1 OR NEW.revoked_at IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM agent_action_approvals p WHERE p.id::text=NEW.resource_id AND p.grant_id=NEW.id AND p.owner_id=NEW.owner_account_id AND p.approved_at IS NULL AND p.consumed_at IS NULL AND p.expires_at=NEW.expires_at AND birdtie_sandbox_action_current(p.id)) THEN RAISE EXCEPTION 'sandbox grant requires actual current preview' USING ERRCODE='23514';END IF;
 ELSE
  IF OLD.purpose<>'OWN_SANDBOX_ACTION' OR (to_jsonb(NEW)-ARRAY['revision','revoked_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['revision','revoked_at']) OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NEW.revision<>OLD.revision+1 OR NEW.revoked_at>clock_timestamp() THEN RAISE EXCEPTION 'sandbox grant only permits revocation' USING ERRCODE='23514';END IF;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_action_grant_guard BEFORE INSERT OR UPDATE ON consent_grants FOR EACH ROW EXECUTE FUNCTION birdtie_sandbox_action_grant_guard();
CREATE FUNCTION birdtie_sandbox_action_dispatch_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p agent_action_approvals;g consent_grants;b jsonb;
BEGIN
 SELECT * INTO p FROM agent_action_approvals WHERE id=NEW.approval_id;
 b:=p.binding_canonical::jsonb;
 IF TG_OP='INSERT' THEN
  SELECT * INTO g FROM consent_grants WHERE id=p.grant_id;
  IF NEW.state<>'DISPATCH_COMMITTED' OR NEW.fence<>0 OR NEW.started_at IS NOT NULL OR NEW.lease_until IS NOT NULL OR NEW.effect_id IS NOT NULL OR NEW.owner_id<>p.owner_id
   OR p.approved_at IS NULL OR p.consumed_at IS NULL OR NOT birdtie_sandbox_action_current(p.id) OR g.id IS NULL OR g.purpose<>'OWN_SANDBOX_ACTION' OR g.revision<>1 OR g.revoked_at IS NOT NULL OR g.expires_at<=clock_timestamp()
   OR NEW.effect_key<>encode(sha256(convert_to('birdtie.sandbox-effect.v1:'||replace(json_build_array(b->>'tenant_id',b->>'logical_operation_id',b->>'action_id','OWN_SANDBOX_WRITE')::text,', ',','),'UTF8')),'hex')
   THEN RAISE EXCEPTION 'dispatch requires current single consumed grant' USING ERRCODE='23514';END IF;
 ELSE
  IF (to_jsonb(NEW)-ARRAY['state','started_at','lease_until','fence','effect_id','applied_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['state','started_at','lease_until','fence','effect_id','applied_at'])
   OR OLD.state IN('SUCCEEDED','NO_EFFECT') OR NEW.fence<OLD.fence OR NEW.fence>OLD.fence+1
   OR NOT((OLD.state='DISPATCH_COMMITTED' AND NEW.state IN('IN_FLIGHT','NO_EFFECT','UNKNOWN_OUTCOME')) OR (OLD.state IN('IN_FLIGHT','UNKNOWN_OUTCOME') AND NEW.state IN('UNKNOWN_OUTCOME','SUCCEEDED','NO_EFFECT')))
   OR (NEW.state='IN_FLIGHT' AND (OLD.started_at IS NOT NULL OR NEW.fence<>OLD.fence+1 OR NEW.started_at IS NULL OR NEW.lease_until<=clock_timestamp() OR NEW.lease_until>p.expires_at))
   OR (NEW.state='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM agent_sandbox_writes w WHERE w.dispatch_id=NEW.id AND w.owner_id=NEW.owner_id AND w.id=NEW.effect_id AND w.effect_key=NEW.effect_key AND w.applied_at=NEW.applied_at))
   OR (NEW.state='NO_EFFECT' AND EXISTS(SELECT 1 FROM agent_sandbox_writes w WHERE w.dispatch_id=NEW.id))
   THEN RAISE EXCEPTION 'invalid dispatch transition' USING ERRCODE='23514';END IF;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_action_dispatch_guard BEFORE INSERT OR UPDATE ON agent_action_dispatches FOR EACH ROW EXECUTE FUNCTION birdtie_sandbox_action_dispatch_guard();
CREATE FUNCTION birdtie_sandbox_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' OR NOT EXISTS(SELECT 1 FROM agent_action_dispatches d JOIN agent_action_approvals p ON p.id=d.approval_id WHERE d.id=NEW.dispatch_id AND d.owner_id=NEW.owner_id AND NEW.target_id=p.owner_id AND d.effect_key=NEW.effect_key AND d.state='IN_FLIGHT' AND d.lease_until>clock_timestamp() AND NEW.value=p.payload_canonical::jsonb->>'value') THEN RAISE EXCEPTION 'sandbox effect requires original live dispatch fence' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_sandbox_write_guard BEFORE INSERT OR UPDATE ON agent_sandbox_writes FOR EACH ROW EXECUTE FUNCTION birdtie_sandbox_write_guard();
-- Deferred constraints sort the actual last database clock with confirmation
-- and consumption. Existing consent/identity locks prevent authority ABA.
CREATE FUNCTION birdtie_sandbox_action_atomic_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p agent_action_approvals;
BEGIN
 SELECT * INTO p FROM agent_action_approvals WHERE id=NEW.id;
 IF (p.approved_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.resource_id=p.id::text AND g.purpose='OWN_SANDBOX_ACTION')) OR (p.consumed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agent_action_dispatches d WHERE d.approval_id=p.id)) THEN RAISE EXCEPTION 'sandbox approval missing atomic grant/dispatch' USING ERRCODE='23514';END IF;
 IF p.cancelled_at IS NULL AND (TG_OP='INSERT' OR OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at) AND NOT birdtie_sandbox_action_current(p.id) THEN RAISE EXCEPTION 'sandbox source expired before actual commit' USING ERRCODE='23514';END IF;
 IF (TG_OP='UPDATE' AND (OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at)) AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='OWN_SANDBOX_ACTION' AND g.actions=ARRAY['sandbox_write']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'grant ceased before actual dispatch commit' USING ERRCODE='23514';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER agent_action_atomic_guard AFTER INSERT OR UPDATE ON agent_action_approvals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_sandbox_action_atomic_guard();
COMMIT;
