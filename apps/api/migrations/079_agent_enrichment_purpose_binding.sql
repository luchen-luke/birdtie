BEGIN;
-- Original consent_grants owns every authorization state/revision/expiry.
CREATE TABLE agent_enrichment_purpose_previews (
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id),
 agent_id uuid NOT NULL,
 owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 session_id uuid NOT NULL REFERENCES sessions(id),
 task_id uuid NOT NULL REFERENCES agent_tasks(id),
 moment_id uuid NOT NULL REFERENCES moments(id),
 selection jsonb NOT NULL CHECK(jsonb_typeof(selection)='object' AND octet_length(selection::text)<=2048
   AND NOT selection?'content' AND NOT selection?'taskQuery'),
 authority text NOT NULL CHECK(authority ~ '^[0-9a-f]{64}$'),
 source_binding text NOT NULL CHECK(source_binding ~ '^[0-9a-f]{64}$'),
 task_binding text NOT NULL CHECK(task_binding ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL CHECK(isfinite(observed_at)),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type),
 CHECK(expires_at>observed_at AND expires_at<=observed_at+interval '5 minutes')
);
CREATE INDEX agent_enrichment_purpose_previews_owner ON agent_enrichment_purpose_previews(owner_id,observed_at);
CREATE TABLE agent_enrichment_purpose_bindings (
 grant_id uuid PRIMARY KEY REFERENCES consent_grants(id),
 preview_id uuid NOT NULL UNIQUE REFERENCES agent_enrichment_purpose_previews(id)
);
CREATE FUNCTION birdtie_enrichment_preview_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'analysis preview is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.id='00000000-0000-0000-0000-000000000000'::uuid
  OR NEW.selection->>'taskId' IS DISTINCT FROM NEW.task_id::text
  OR NEW.selection->>'momentId' IS DISTINCT FROM NEW.moment_id::text
  OR jsonb_typeof(NEW.selection->'fields') IS DISTINCT FROM 'array'
  OR jsonb_array_length(NEW.selection->'fields') NOT BETWEEN 1 AND 2
  OR NEW.selection->'fields' NOT IN ('["body"]'::jsonb,'["title"]'::jsonb,'["body","title"]'::jsonb)
  OR NOT EXISTS(SELECT 1 FROM accounts a JOIN sessions s ON s.account_id=a.id
   JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_tasks t ON t.owner_account_id=a.id
   JOIN moments m ON m.author_account_id=a.id
   WHERE a.id=NEW.owner_id AND a.account_type='person' AND a.status='active'
    AND s.id=NEW.session_id AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
    AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active'
    AND t.id=NEW.task_id AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.status='ACTIVE'
    AND m.id=NEW.moment_id AND m.visibility='private' AND m.status='draft'
    AND m.revision=(NEW.selection->>'momentRevision')::bigint
    AND NEW.expires_at<=m.updated_at+interval '15 minutes'
    AND NEW.expires_at<=s.expires_at AND NEW.expires_at<=s.idle_expires_at
    AND NEW.expires_at<=(NEW.selection->>'deadlineAt')::timestamptz)
 THEN RAISE EXCEPTION 'analysis preview needs current exact native source' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_enrichment_preview_guard BEFORE INSERT OR UPDATE ON agent_enrichment_purpose_previews
 FOR EACH ROW EXECUTE FUNCTION birdtie_enrichment_preview_guard();
CREATE FUNCTION birdtie_enrichment_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'analysis binding is immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_enrichment_purpose_previews p ON p.id=NEW.preview_id
  WHERE g.id=NEW.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id
   AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='MOMENT_LOCAL_ANALYSIS'
   AND g.actions=ARRAY['analyze_local']::text[] AND g.revision=1 AND g.revoked_at IS NULL
   AND g.expires_at>clock_timestamp() AND g.expires_at<=p.expires_at)
 THEN RAISE EXCEPTION 'analysis binding needs native original grant' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_enrichment_binding_guard BEFORE INSERT OR UPDATE ON agent_enrichment_purpose_bindings
 FOR EACH ROW EXECUTE FUNCTION birdtie_enrichment_binding_guard();
CREATE FUNCTION birdtie_enrichment_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND NEW.purpose<>'MOMENT_LOCAL_ANALYSIS' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND OLD.purpose<>'MOMENT_LOCAL_ANALYSIS' AND NEW.purpose<>'MOMENT_LOCAL_ANALYSIS' THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.revision<>1 OR NEW.revoked_at IS NOT NULL OR NEW.resource_type<>'agent_context'
   OR NEW.owner_account_id IS DISTINCT FROM NEW.recipient_account_id OR NEW.actions<>ARRAY['analyze_local']::text[]
   OR NEW.expires_at IS NULL OR NOT isfinite(NEW.expires_at) OR NOT isfinite(NEW.created_at)
   OR NEW.created_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp() OR NEW.expires_at<=NEW.created_at
   OR NOT EXISTS(SELECT 1 FROM agent_enrichment_purpose_previews p WHERE p.id::text=NEW.resource_id
     AND p.owner_id=NEW.owner_account_id AND p.expires_at>=NEW.expires_at)
  THEN RAISE EXCEPTION 'invalid local analysis grant' USING ERRCODE='23514'; END IF;
 ELSE
  IF OLD.purpose<>'MOMENT_LOCAL_ANALYSIS' OR NEW.id IS DISTINCT FROM OLD.id
   OR NEW.owner_account_id IS DISTINCT FROM OLD.owner_account_id OR NEW.recipient_account_id IS DISTINCT FROM OLD.recipient_account_id
   OR NEW.resource_type IS DISTINCT FROM OLD.resource_type OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
   OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.actions IS DISTINCT FROM OLD.actions
   OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
   OR OLD.revoked_at IS NOT NULL OR OLD.revision=9223372036854775807 OR NEW.revision<>OLD.revision+1
   OR NEW.revoked_at IS NULL OR NOT isfinite(NEW.revoked_at) OR NEW.revoked_at<OLD.created_at OR NEW.revoked_at>clock_timestamp()
  THEN RAISE EXCEPTION 'analysis grant permits only irreversible versioned revoke' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_enrichment_grant_guard BEFORE INSERT OR UPDATE ON consent_grants
 FOR EACH ROW EXECUTE FUNCTION birdtie_enrichment_grant_guard();
COMMIT;
