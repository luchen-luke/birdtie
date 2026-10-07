BEGIN;
-- Derived counters only: original audits/business/consents/fees remain authority.
CREATE TABLE agent_enrichment_observations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 audit_event_id bigint UNIQUE REFERENCES audit_events(id) ON DELETE CASCADE,
 admin_audit_event_id bigint UNIQUE REFERENCES admin_audit_events(id) ON DELETE CASCADE,
 owner_type text NOT NULL CHECK(owner_type IN ('PERSON','ORGANIZATION')),
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 metric text NOT NULL CHECK(metric IN ('memory_created','memory_rejected','memory_corrected','memory_deleted','candidate_promoted')),
 occurred_at timestamptz NOT NULL CHECK(isfinite(occurred_at)),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 CHECK(num_nonnulls(audit_event_id,admin_audit_event_id)=1),
 CHECK((owner_type='PERSON')=(audit_event_id IS NOT NULL)),
 CHECK(expires_at>occurred_at AND expires_at<=occurred_at+interval '90 days')
);
CREATE INDEX agent_enrichment_observations_owner_window ON agent_enrichment_observations(owner_type,owner_id,occurred_at,id);
CREATE INDEX agent_enrichment_observations_expiry ON agent_enrichment_observations(expires_at,id);
CREATE FUNCTION birdtie_enrichment_observation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a record; m record; original_xid text; expected text;
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'observation metadata immutable' USING ERRCODE='23514';END IF;
 IF NEW.audit_event_id IS NOT NULL THEN
  SELECT actor_account_id,resource_type,resource_id,action,purpose,decision,occurred_at,xmin::text AS original_xmin INTO a FROM audit_events WHERE id=NEW.audit_event_id;
  IF NOT FOUND OR a.actor_account_id<>NEW.owner_id OR a.decision<>'allowed' OR a.original_xmin<>pg_current_xact_id()::xid::text OR a.occurred_at<>NEW.occurred_at THEN RAISE EXCEPTION 'same native transaction audit required' USING ERRCODE='23514';END IF;
  IF a.resource_type='agent_memory' AND a.purpose IN('human_explicit_memory_edit','human_memory_delete') THEN
   SELECT owner_id,agent_id,version,status,source_type,xmin::text AS original_xmin INTO m FROM agent_memories WHERE id=a.resource_id::uuid AND owner_type='PERSON';
   IF a.action='put' AND a.purpose='human_explicit_memory_edit' AND m.status='ACTIVE' AND m.source_type='EXPLICIT' THEN expected:=CASE WHEN m.version=1 THEN 'memory_created' ELSE 'memory_corrected' END;
   ELSIF a.action='delete' AND a.purpose='human_memory_delete' AND m.status='DELETED' THEN expected:='memory_deleted';END IF;
  ELSIF a.resource_type='memory_candidate' AND a.purpose='human_candidate_decision' THEN
   SELECT owner_id,agent_id,status,xmin::text AS original_xmin INTO m FROM agent_memory_candidates WHERE id=a.resource_id::uuid;
   IF a.action='reject' AND m.status='REJECTED' THEN expected:='memory_rejected';
   ELSIF a.action='approve' AND m.status='ACTIVE' AND EXISTS(SELECT 1 FROM agent_memory_candidates c JOIN agent_memories mem ON mem.id=c.memory_id AND mem.version=c.memory_version WHERE c.id=a.resource_id::uuid AND mem.owner_id=NEW.owner_id AND mem.agent_id=NEW.agent_id AND mem.status='ACTIVE' AND mem.source_type='EXPLICIT') THEN expected:='candidate_promoted';END IF;
  END IF;
 ELSE
  SELECT actor_account_id,organization_id,resource_type,resource_id,action,occurred_at,xmin::text AS original_xmin INTO a FROM admin_audit_events WHERE id=NEW.admin_audit_event_id;
  IF NOT FOUND OR a.resource_type<>'organization_memory' OR a.original_xmin<>pg_current_xact_id()::xid::text OR a.occurred_at<>NEW.occurred_at OR NOT EXISTS(SELECT 1 FROM organizations o WHERE o.id=a.organization_id AND o.account_id=NEW.owner_id) THEN RAISE EXCEPTION 'same native organization audit required' USING ERRCODE='23514';END IF;
  SELECT owner_id,agent_id,version,status,source_type,xmin::text AS original_xmin INTO m FROM agent_memories WHERE id=a.resource_id AND owner_type='ORGANIZATION';
  IF a.action='organization_memory_put' AND m.status='ACTIVE' AND m.source_type='EXPLICIT' THEN expected:=CASE WHEN m.version=1 THEN 'memory_created' ELSE 'memory_corrected' END;
  ELSIF a.action='organization_memory_delete' AND m.status='DELETED' THEN expected:='memory_deleted';END IF;
 END IF;
 IF expected IS NULL OR expected<>NEW.metric OR m.owner_id IS DISTINCT FROM NEW.owner_id OR m.agent_id IS DISTINCT FROM NEW.agent_id OR m.original_xmin IS DISTINCT FROM pg_current_xact_id()::xid::text OR NEW.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'exact committed lifecycle observation required' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_enrichment_observation_guard BEFORE INSERT OR UPDATE ON agent_enrichment_observations FOR EACH ROW EXECUTE FUNCTION birdtie_enrichment_observation_guard();
COMMIT;
