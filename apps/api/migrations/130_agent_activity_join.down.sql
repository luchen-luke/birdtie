BEGIN;
DO $$BEGIN IF EXISTS(SELECT 1 FROM agent_action_approvals WHERE action_kind='ACTIVITY_JOIN') OR EXISTS(SELECT 1 FROM agent_activity_join_effects) OR EXISTS(SELECT 1 FROM consent_grants WHERE purpose='OWN_ACTIVITY_JOIN_ACTION') OR EXISTS(SELECT 1 FROM audit_events WHERE purpose='OWN_ACTIVITY_JOIN_ACTION') THEN RAISE EXCEPTION 'used Agent JOIN history refuses down' USING ERRCODE='55000';END IF;END $$;
CREATE OR REPLACE FUNCTION birdtie_sandbox_action_approval_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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

CREATE OR REPLACE FUNCTION birdtie_sandbox_action_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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

CREATE OR REPLACE FUNCTION birdtie_sandbox_action_dispatch_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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

CREATE OR REPLACE FUNCTION birdtie_sandbox_action_atomic_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p agent_action_approvals;
BEGIN
 SELECT * INTO p FROM agent_action_approvals WHERE id=NEW.id;
 IF (p.approved_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.resource_id=p.id::text AND g.purpose='OWN_SANDBOX_ACTION')) OR (p.consumed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agent_action_dispatches d WHERE d.approval_id=p.id)) THEN RAISE EXCEPTION 'sandbox approval missing atomic grant/dispatch' USING ERRCODE='23514';END IF;
 IF p.cancelled_at IS NULL AND (TG_OP='INSERT' OR OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at) AND NOT birdtie_sandbox_action_current(p.id) THEN RAISE EXCEPTION 'sandbox source expired before actual commit' USING ERRCODE='23514';END IF;
 IF (TG_OP='UPDATE' AND (OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at)) AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='OWN_SANDBOX_ACTION' AND g.actions=ARRAY['sandbox_write']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'grant ceased before actual dispatch commit' USING ERRCODE='23514';END IF;
 RETURN NULL;
END $$;
DROP TABLE agent_activity_join_effects;
DROP FUNCTION birdtie_activity_join_effect_guard();
DROP FUNCTION birdtie_activity_join_effect_atomic_guard();
DROP FUNCTION birdtie_activity_join_dispatch_check(agent_action_dispatches,agent_action_dispatches,text);
DROP FUNCTION birdtie_activity_join_grant_check(consent_grants,consent_grants,text);
DROP FUNCTION birdtie_activity_join_approval_check(agent_action_approvals,agent_action_approvals,text);
DROP FUNCTION birdtie_activity_join_effect_current(uuid);
DROP FUNCTION birdtie_activity_join_current(uuid);
DROP FUNCTION birdtie_activity_join_base_current(uuid);
DROP FUNCTION birdtie_activity_join_authority(uuid,uuid,uuid);
DROP FUNCTION birdtie_activity_join_reader_authority(bytea,uuid,uuid);
DROP FUNCTION birdtie_activity_join_target_tail(uuid,uuid);
DROP FUNCTION birdtie_activity_join_participant_version(uuid,uuid);
ALTER TABLE agent_action_approvals DROP COLUMN action_kind;
COMMIT;
