BEGIN;
-- Add one closed native JOIN adapter to the original approval/dispatch ledger.
-- No Human receipt is reclassified and no Agent account is manufactured.
ALTER TABLE agent_action_approvals ADD COLUMN action_kind text NOT NULL DEFAULT 'SANDBOX'
 CONSTRAINT agent_action_approval_kind130 CHECK(action_kind IN('SANDBOX','ACTIVITY_JOIN'));
CREATE TABLE agent_activity_join_effects(
 id uuid PRIMARY KEY,dispatch_id uuid NOT NULL UNIQUE REFERENCES agent_action_dispatches(id) ON DELETE RESTRICT,
 approval_id uuid NOT NULL UNIQUE REFERENCES agent_action_approvals(id) ON DELETE RESTRICT,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE RESTRICT,
 activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE RESTRICT,
 participation_id uuid NOT NULL REFERENCES activity_participations(id) ON DELETE RESTRICT,
 effect_key text NOT NULL CHECK(effect_key~'^[0-9a-f]{64}$'),state text NOT NULL CHECK(state='going'),
 participation_updated_at timestamptz NOT NULL,applied_at timestamptz NOT NULL,
 UNIQUE(owner_id,effect_key),CHECK(isfinite(applied_at) AND participation_updated_at<=applied_at)
);
CREATE FUNCTION birdtie_activity_join_participant_version(aid uuid,oid uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text) ORDER BY p.id),'[]'::jsonb)::text,'UTF8')),'hex') FROM activity_participations p WHERE p.activity_id=aid AND p.participant_account_id=oid $$;
CREATE FUNCTION birdtie_activity_join_target_tail(aid uuid,oid uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(kind,id,epoch) ORDER BY kind,id),'[]'::jsonb)::text,'UTF8')),'hex') FROM (
 SELECT 'activity' kind,a.id::text id,a.xmin::text epoch FROM activities a WHERE a.id=aid
 UNION ALL SELECT 'city',c.id,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'organizer',o.activity_id::text,o.xmin::text FROM activity_organizers o WHERE o.activity_id=aid
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id=oid OR a.id IN(SELECT host_account_id FROM activities WHERE id=aid UNION SELECT person_account_id FROM activity_organizers WHERE activity_id=aid UNION SELECT account_id FROM organizations WHERE id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=aid) UNION SELECT account_id FROM businesses WHERE id IN(SELECT business_id FROM activity_organizers WHERE activity_id=aid) UNION SELECT owner_account_id FROM communities WHERE id IN(SELECT community_id FROM activity_organizers WHERE activity_id=aid))
 UNION ALL SELECT 'place',p.id::text,p.xmin::text FROM places p WHERE p.id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'org',o.id::text,o.xmin::text FROM organizations o WHERE o.id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'orgmember',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id IN(SELECT business_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN(SELECT community_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'communitymember',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT community_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'blocks',b.blocker_account_id::text||':'||b.blocked_account_id::text,b.xmin::text FROM account_blocks b WHERE b.blocker_account_id=oid OR b.blocked_account_id=oid
 UNION ALL SELECT 'participant',p.id::text,p.xmin::text FROM activity_participations p WHERE p.activity_id=aid AND p.participant_account_id<>oid
 UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'venuecandidate',v.id::text,v.xmin::text FROM venue_candidates v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'businessvenue',v.business_id::text||':'||v.place_id::text,v.xmin::text FROM business_venue_relations v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=aid)
 ) source_versions $$;
CREATE FUNCTION birdtie_activity_join_reader_authority(digest bytea,oid uuid,aid uuid) RETURNS text LANGUAGE sql VOLATILE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('session',to_jsonb(s)-'idle_expires_at','account',to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text),'agent',to_jsonb(ag)||jsonb_build_object('_xmin',ag.xmin::text),'profile',to_jsonb(ap)||jsonb_build_object('_xmin',ap.xmin::text))::text,'UTF8')),'hex')
 FROM sessions s JOIN accounts a ON a.id=s.account_id JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE s.token_sha256=digest AND a.id=oid AND ag.id=aid AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp() $$;
CREATE FUNCTION birdtie_activity_join_authority(sid uuid,oid uuid,aid uuid) RETURNS text LANGUAGE sql VOLATILE AS $$
 SELECT birdtie_activity_join_reader_authority(s.token_sha256,oid,aid) FROM sessions s WHERE s.id=sid $$;
CREATE FUNCTION birdtie_activity_join_base_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM agent_action_approvals p JOIN sessions s ON s.id=p.session_id JOIN accounts a ON a.id=p.owner_id JOIN agents ag ON ag.id=p.agent_id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN agent_tasks t ON t.id=p.task_id JOIN activities ac ON ac.id=(p.binding_canonical::jsonb->>'target_id')::uuid JOIN cities city ON city.id=ac.city_id CROSS JOIN LATERAL(SELECT p.binding_canonical::jsonb b) x
 WHERE p.id=pid AND p.action_kind='ACTIVITY_JOIN' AND s.account_id=a.id AND a.account_type='person' AND a.status='active' AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' AND t.owner_account_id=a.id AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.status='ACTIVE' AND t.xmin::text=x.b->>'source_generation'
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=a.id AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id)
 AND t.intent IN('FIND_ACTIVITY','AREA_DISCOVERY','REFINE_RESULTS','COMPARE_RESULTS') AND t.city_id=ac.city_id
 AND birdtie_activity_join_reader_authority(s.token_sha256,a.id,ag.id) IS NOT NULL AND birdtie_activity_join_authority(s.id,a.id,ag.id)=x.b->>'authority_version'
 AND (SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(ps)||jsonb_build_object('_xmin',ps.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings ps WHERE ps.owner_id=a.id AND ps.agent_id=ag.id AND ps.owner_type='PERSON')=x.b->>'policy_version'
 AND birdtie_activity_join_target_tail(ac.id,a.id)=x.b->>'target_tail_version' AND p.cancelled_at IS NULL AND p.expires_at>clock_timestamp()
 AND ac.visibility='public' AND ac.publication_status='published' AND ac.cancelled_at IS NULL AND ac.ends_at>clock_timestamp() AND (ac.expires_at IS NULL OR ac.expires_at>clock_timestamp()) AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(ac.id,a.id)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE ac.host_account_id IS NOT NULL AND ((bl.blocker_account_id=a.id AND bl.blocked_account_id=ac.host_account_id) OR(bl.blocked_account_id=a.id AND bl.blocker_account_id=ac.host_account_id)))) $$;
CREATE FUNCTION birdtie_activity_join_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT birdtie_activity_join_base_current(pid) AND EXISTS(SELECT 1 FROM agent_action_approvals p CROSS JOIN LATERAL(SELECT p.binding_canonical::jsonb b) x WHERE p.id=pid AND birdtie_activity_join_participant_version((x.b->>'target_id')::uuid,p.owner_id)=x.b->>'participation_version' AND NOT EXISTS(SELECT 1 FROM activity_participations r WHERE r.activity_id=(x.b->>'target_id')::uuid AND r.participant_account_id=p.owner_id AND r.status IN('going','pending'))) $$;
CREATE FUNCTION birdtie_activity_join_effect_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT birdtie_activity_join_base_current(pid) AND EXISTS(SELECT 1 FROM agent_action_approvals p JOIN agent_action_dispatches d ON d.approval_id=p.id JOIN agent_activity_join_effects w ON w.dispatch_id=d.id JOIN activity_participations r ON r.id=w.participation_id JOIN activities ac ON ac.id=w.activity_id JOIN consent_grants g ON g.id=p.grant_id
 WHERE p.id=pid AND d.state='IN_FLIGHT' AND d.lease_until>clock_timestamp() AND w.owner_id=p.owner_id AND w.agent_id=p.agent_id AND r.participant_account_id=p.owner_id AND r.activity_id=w.activity_id AND r.status='going' AND r.updated_at=w.participation_updated_at AND r.xmin::text=mod(pg_current_xact_id()::text::numeric,4294967296)::text
 AND w.xmin::text=mod(pg_current_xact_id()::text::numeric,4294967296)::text AND g.purpose='OWN_ACTIVITY_JOIN_ACTION' AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_id=p.id::text AND g.resource_type='agent_context' AND g.actions=ARRAY['activity_join']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()
 AND (ac.capacity IS NULL OR ac.capacity>=(SELECT count(*) FROM activity_participations cp WHERE cp.activity_id=ac.id AND cp.status IN('going','pending')))) $$;
CREATE FUNCTION birdtie_activity_join_approval_check(n agent_action_approvals,o agent_action_approvals,op text) RETURNS agent_action_approvals LANGUAGE plpgsql AS $$
DECLARE b jsonb;v jsonb;
BEGIN
 b:=n.binding_canonical::jsonb;v:=n.payload_canonical::jsonb;
 IF op='UPDATE' THEN
  IF o.action_kind<>'ACTIVITY_JOIN' OR (to_jsonb(n)-ARRAY['approved_at','consumed_at','cancelled_at']) IS DISTINCT FROM (to_jsonb(o)-ARRAY['approved_at','consumed_at','cancelled_at']) OR(o.approved_at IS NOT NULL AND n.approved_at IS DISTINCT FROM o.approved_at) OR(o.consumed_at IS NOT NULL AND n.consumed_at IS DISTINCT FROM o.consumed_at) OR(o.cancelled_at IS NOT NULL AND n.cancelled_at IS DISTINCT FROM o.cancelled_at) OR(n.consumed_at IS NOT NULL AND n.approved_at IS NULL) THEN RAISE EXCEPTION 'immutable Agent JOIN approval' USING ERRCODE='23514';END IF;
 ELSE IF n.approved_at IS NOT NULL OR n.consumed_at IS NOT NULL OR n.cancelled_at IS NOT NULL THEN RAISE EXCEPTION 'JOIN preview is not approved' USING ERRCODE='23514';END IF;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(b))<>28 OR (SELECT count(*) FROM jsonb_object_keys(v))<>4
 OR EXISTS(SELECT 1 FROM jsonb_each(b) f WHERE(f.key='grant_revision' AND f.value::text<>'1') OR(f.key<>'grant_revision' AND jsonb_typeof(f.value)<>'string'))
 OR EXISTS(SELECT 1 FROM jsonb_each(v) f WHERE jsonb_typeof(f.value)<>'string')
 OR NOT(b ?& ARRAY['schema_version','approval_id','tenant_id','actor_id','subject_type','subject_id','agent_id','session_id','task_id','logical_operation_id','action_id','tool','tool_version','target_id','payload_digest','source_version','source_generation','authority_version','policy_version','consent_purpose','grant_id','grant_revision','membership_version','observed_at','expires_at','target_version','target_tail_version','participation_version'])
 OR NOT(v ?& ARRAY['taskId','logicalOperationId','actionId','activityId']) OR b->>'schema_version'<>'air.activity_join.v1' OR b->>'approval_id'<>n.id::text OR b->>'tenant_id'<>n.owner_id::text OR b->>'actor_id'<>n.owner_id::text OR b->>'subject_id'<>n.owner_id::text OR b->>'subject_type'<>'PERSON' OR b->>'agent_id'<>n.agent_id::text OR b->>'task_id'<>n.task_id::text OR b->>'session_id'<>n.session_id::text
 OR b->>'tool'<>'activity.join' OR b->>'tool_version'<>'activity.join.v1' OR b->>'consent_purpose'<>'OWN_ACTIVITY_JOIN_ACTION' OR b->>'grant_id'<>n.grant_id::text OR b->>'grant_revision'<>'1' OR b->>'membership_version'<>'NONE_PERSON_ONLY'
 OR b->>'task_id'<>v->>'taskId' OR b->>'logical_operation_id'<>v->>'logicalOperationId' OR b->>'action_id'<>v->>'actionId' OR b->>'target_id'<>v->>'activityId'
 OR (b->>'observed_at')::timestamptz<>n.observed_at OR (b->>'expires_at')::timestamptz<>n.expires_at
 OR b->>'payload_digest'<>encode(sha256(convert_to('birdtie.activity-join-payload.v1:'||n.payload_canonical,'UTF8')),'hex') OR n.binding_digest<>encode(sha256(convert_to('birdtie.activity-join-approval.v1:'||n.binding_canonical,'UTF8')),'hex')
 OR EXISTS(SELECT 1 FROM unnest(ARRAY['source_version','authority_version','policy_version','target_version','target_tail_version','participation_version']) k WHERE (b->>k)!~'^[0-9a-f]{64}$')
 THEN RAISE EXCEPTION 'invalid closed Agent JOIN binding' USING ERRCODE='23514';END IF;RETURN n;
END $$;
CREATE FUNCTION birdtie_activity_join_grant_check(n consent_grants,o consent_grants,op text) RETURNS consent_grants LANGUAGE plpgsql AS $$
BEGIN
 IF op='INSERT' THEN
  IF n.purpose<>'OWN_ACTIVITY_JOIN_ACTION' OR n.resource_type<>'agent_context' OR n.owner_account_id IS DISTINCT FROM n.recipient_account_id OR n.actions<>ARRAY['activity_join']::text[] OR n.revision<>1 OR n.revoked_at IS NOT NULL OR NOT EXISTS(SELECT 1 FROM agent_action_approvals p WHERE p.id::text=n.resource_id AND p.action_kind='ACTIVITY_JOIN' AND p.grant_id=n.id AND p.owner_id=n.owner_account_id AND p.approved_at IS NULL AND p.consumed_at IS NULL AND p.expires_at=n.expires_at AND birdtie_activity_join_current(p.id)) THEN RAISE EXCEPTION 'JOIN grant requires current exact preview' USING ERRCODE='23514';END IF;
 ELSE
  IF o.purpose<>'OWN_ACTIVITY_JOIN_ACTION' OR (to_jsonb(n)-ARRAY['revision','revoked_at']) IS DISTINCT FROM (to_jsonb(o)-ARRAY['revision','revoked_at']) OR o.revoked_at IS NOT NULL OR n.revoked_at IS NULL OR n.revision<>o.revision+1 OR n.revoked_at>clock_timestamp() THEN RAISE EXCEPTION 'JOIN grant only permits revocation' USING ERRCODE='23514';END IF;
 END IF;RETURN n;
END $$;
CREATE FUNCTION birdtie_activity_join_dispatch_check(n agent_action_dispatches,o agent_action_dispatches,op text) RETURNS agent_action_dispatches LANGUAGE plpgsql AS $$
DECLARE p agent_action_approvals;g consent_grants;b jsonb;
BEGIN
 SELECT * INTO p FROM agent_action_approvals WHERE id=n.approval_id;b:=p.binding_canonical::jsonb;
 IF op='INSERT' THEN
  SELECT * INTO g FROM consent_grants WHERE id=p.grant_id;
  IF p.action_kind<>'ACTIVITY_JOIN' OR n.state<>'DISPATCH_COMMITTED' OR n.fence<>0 OR n.started_at IS NOT NULL OR n.lease_until IS NOT NULL OR n.effect_id IS NOT NULL OR n.owner_id<>p.owner_id OR p.approved_at IS NULL OR p.consumed_at IS NULL OR NOT birdtie_activity_join_current(p.id) OR g.id IS NULL OR g.purpose<>'OWN_ACTIVITY_JOIN_ACTION' OR g.owner_account_id<>p.owner_id OR g.recipient_account_id<>p.owner_id OR g.resource_id<>p.id::text OR g.actions<>ARRAY['activity_join']::text[] OR g.revision<>1 OR g.revoked_at IS NOT NULL OR g.expires_at<=clock_timestamp()
  OR n.effect_key<>encode(sha256(convert_to('birdtie.activity-join-effect.v1:'||replace(json_build_array(b->>'tenant_id',b->>'logical_operation_id',b->>'action_id','ACTIVITY_JOIN')::text,', ',','),'UTF8')),'hex') THEN RAISE EXCEPTION 'JOIN dispatch needs current consumed grant' USING ERRCODE='23514';END IF;
 ELSE
  IF (to_jsonb(n)-ARRAY['state','started_at','lease_until','fence','effect_id','applied_at']) IS DISTINCT FROM (to_jsonb(o)-ARRAY['state','started_at','lease_until','fence','effect_id','applied_at']) OR o.state IN('SUCCEEDED','NO_EFFECT') OR n.fence<o.fence OR n.fence>o.fence+1 OR NOT((o.state='DISPATCH_COMMITTED' AND n.state IN('IN_FLIGHT','NO_EFFECT','UNKNOWN_OUTCOME')) OR(o.state IN('IN_FLIGHT','UNKNOWN_OUTCOME') AND n.state IN('SUCCEEDED','NO_EFFECT','UNKNOWN_OUTCOME')))
  OR(n.state='IN_FLIGHT' AND (o.started_at IS NOT NULL OR n.fence<>o.fence+1 OR n.started_at IS NULL OR n.lease_until<=clock_timestamp() OR n.lease_until>p.expires_at OR NOT birdtie_activity_join_current(p.id)))
  OR(n.state='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM agent_activity_join_effects w WHERE w.dispatch_id=n.id AND w.approval_id=p.id AND w.owner_id=n.owner_id AND w.agent_id=p.agent_id AND w.id=n.effect_id AND w.effect_key=n.effect_key AND w.applied_at=n.applied_at))
  OR(n.state='NO_EFFECT' AND EXISTS(SELECT 1 FROM agent_activity_join_effects w WHERE w.dispatch_id=n.id)) OR(n.state='NO_EFFECT' AND n.fence<>o.fence+1)
  THEN RAISE EXCEPTION 'invalid JOIN causal dispatch transition' USING ERRCODE='23514';END IF;
 END IF;RETURN n;
END $$;
CREATE FUNCTION birdtie_activity_join_effect_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p agent_action_approvals;d agent_action_dispatches;r activity_participations;xid_text text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Agent JOIN effect is immutable' USING ERRCODE='55000';END IF;
 SELECT * INTO d FROM agent_action_dispatches WHERE id=NEW.dispatch_id FOR UPDATE;SELECT * INTO p FROM agent_action_approvals WHERE id=d.approval_id;SELECT * INTO r FROM activity_participations WHERE id=NEW.participation_id;
 xid_text:=mod(pg_current_xact_id()::text::numeric,4294967296)::text;
 IF d.id IS NULL OR p.id IS NULL OR p.action_kind<>'ACTIVITY_JOIN' OR d.state<>'IN_FLIGHT' OR d.lease_until<=clock_timestamp() OR NOT birdtie_activity_join_base_current(p.id) OR NEW.approval_id<>p.id OR NEW.owner_id<>p.owner_id OR NEW.agent_id<>p.agent_id OR NEW.task_id<>p.task_id OR NEW.activity_id::text<>p.binding_canonical::jsonb->>'target_id' OR NEW.effect_key<>d.effect_key OR r.id IS NULL OR r.participant_account_id<>NEW.owner_id OR r.activity_id<>NEW.activity_id OR r.status<>'going' OR r.updated_at<>NEW.participation_updated_at OR NEW.applied_at>clock_timestamp()
 OR NOT EXISTS(SELECT 1 FROM activity_participations native WHERE native.id=r.id AND native.xmin::text=xid_text)
 OR NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.purpose='OWN_ACTIVITY_JOIN_ACTION' AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_id=p.id::text AND g.revision=1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp())
 OR NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.actor_account_id=NEW.owner_id AND a.action='join' AND a.resource_type='activity_participation' AND a.resource_id=r.id::text AND a.target_resource_id=NEW.activity_id AND a.purpose='agent_activity_join' AND a.decision='allowed' AND a.xmin::text=xid_text)
 THEN RAISE EXCEPTION 'JOIN effect requires original transition, grant and audit' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_activity_join_effect_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_activity_join_effects FOR EACH ROW EXECUTE FUNCTION birdtie_activity_join_effect_guard();
CREATE TRIGGER agent_activity_join_effect_truncate_guard BEFORE TRUNCATE ON agent_activity_join_effects FOR EACH STATEMENT EXECUTE FUNCTION birdtie_activity_join_effect_guard();
CREATE OR REPLACE FUNCTION birdtie_sandbox_action_approval_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE b jsonb;v jsonb;
BEGIN
 IF NEW.action_kind='ACTIVITY_JOIN' THEN RETURN birdtie_activity_join_approval_check(NEW,OLD,TG_OP);END IF;
 IF NEW.action_kind<>'SANDBOX' OR(TG_OP='UPDATE' AND OLD.action_kind<>'SANDBOX') THEN RAISE EXCEPTION 'closed action kind is immutable' USING ERRCODE='23514';END IF;
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
 IF NEW.purpose='OWN_ACTIVITY_JOIN_ACTION' OR(TG_OP='UPDATE' AND OLD.purpose='OWN_ACTIVITY_JOIN_ACTION') THEN RETURN birdtie_activity_join_grant_check(NEW,OLD,TG_OP);END IF;
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
 IF p.action_kind='ACTIVITY_JOIN' THEN RETURN birdtie_activity_join_dispatch_check(NEW,OLD,TG_OP);END IF;
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
 IF p.action_kind='ACTIVITY_JOIN' THEN
  IF(p.approved_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.resource_id=p.id::text AND g.purpose='OWN_ACTIVITY_JOIN_ACTION')) OR(p.consumed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agent_action_dispatches d WHERE d.approval_id=p.id)) THEN RAISE EXCEPTION 'JOIN missing atomic grant/dispatch' USING ERRCODE='23514';END IF;
  IF p.cancelled_at IS NULL AND(TG_OP='INSERT' OR OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at) AND NOT birdtie_activity_join_current(p.id) THEN RAISE EXCEPTION 'JOIN authority expired before commit' USING ERRCODE='23514';END IF;
  IF p.cancelled_at IS NULL AND TG_OP='UPDATE' AND(OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at) AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_id=p.id::text AND g.resource_type='agent_context' AND g.purpose='OWN_ACTIVITY_JOIN_ACTION' AND g.actions=ARRAY['activity_join']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'JOIN grant ceased before commit' USING ERRCODE='23514';END IF;
  RETURN NULL;
 END IF;
 IF (p.approved_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.resource_id=p.id::text AND g.purpose='OWN_SANDBOX_ACTION')) OR (p.consumed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agent_action_dispatches d WHERE d.approval_id=p.id)) THEN RAISE EXCEPTION 'sandbox approval missing atomic grant/dispatch' USING ERRCODE='23514';END IF;
 IF p.cancelled_at IS NULL AND (TG_OP='INSERT' OR OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at) AND NOT birdtie_sandbox_action_current(p.id) THEN RAISE EXCEPTION 'sandbox source expired before actual commit' USING ERRCODE='23514';END IF;
 IF (TG_OP='UPDATE' AND (OLD.approved_at IS DISTINCT FROM NEW.approved_at OR OLD.consumed_at IS DISTINCT FROM NEW.consumed_at)) AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.id=p.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='OWN_SANDBOX_ACTION' AND g.actions=ARRAY['sandbox_write']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'grant ceased before actual dispatch commit' USING ERRCODE='23514';END IF;
 RETURN NULL;
END $$;
CREATE FUNCTION birdtie_activity_join_effect_atomic_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT birdtie_activity_join_effect_current(NEW.approval_id) THEN RAISE EXCEPTION 'JOIN final authority/lease/transition expired' USING ERRCODE='23514';END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER agent_activity_join_effect_atomic_guard AFTER INSERT ON agent_activity_join_effects DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_activity_join_effect_atomic_guard();
COMMIT;
