BEGIN;
-- Metadata only until independent exact retention approval. The original
-- consent_grants retains the sole authorization lifecycle.
CREATE TABLE agent_candidate_retention_previews(
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id),
 agent_id uuid NOT NULL,
 owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 session_id uuid NOT NULL REFERENCES sessions(id),
 analysis_grant_id uuid NOT NULL REFERENCES consent_grants(id),
 analysis_preview_id uuid NOT NULL REFERENCES agent_enrichment_purpose_previews(id),
 selection jsonb NOT NULL CHECK(jsonb_typeof(selection)='object' AND octet_length(selection::text)<=2048),
 authority text NOT NULL CHECK(authority ~ '^[0-9a-f]{64}$'),
 source_frame text NOT NULL CHECK(source_frame ~ '^[0-9a-f]{64}$'),
 review_digest text NOT NULL CHECK(review_digest ~ '^[0-9a-f]{64}$'),
 event_id uuid NOT NULL REFERENCES agent_domain_outbox(event_id),
 logical_operation_id uuid NOT NULL,
 algorithm_version text NOT NULL CHECK(algorithm_version='moment-lexical-category-v1'),
 observed_at timestamptz NOT NULL CHECK(isfinite(observed_at)),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type),
 CHECK(expires_at>observed_at AND expires_at<=observed_at+interval '90 seconds')
);
CREATE INDEX agent_candidate_retention_previews_owner ON agent_candidate_retention_previews(owner_id,observed_at);
CREATE TABLE agent_candidate_retention_bindings(
 grant_id uuid PRIMARY KEY REFERENCES consent_grants(id),
 preview_id uuid NOT NULL UNIQUE REFERENCES agent_candidate_retention_previews(id)
);
CREATE FUNCTION birdtie_candidate_retention_preview_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'retention preview is immutable' USING ERRCODE='23514';END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(NEW.selection))<>2
  OR NOT(NEW.selection ?& ARRAY['analysisGrantId','retainUntil'])
  OR NEW.selection->>'analysisGrantId' IS DISTINCT FROM NEW.analysis_grant_id::text
  OR NEW.expires_at>(NEW.selection->>'retainUntil')::timestamptz
  OR NOT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id
    JOIN agent_enrichment_purpose_previews p ON p.id=eb.preview_id
    JOIN accounts a ON a.id=p.owner_id JOIN agents ag ON ag.id=p.agent_id
    JOIN sessions se ON se.id=p.session_id JOIN moments m ON m.id=p.moment_id
    JOIN agent_tasks t ON t.id=p.task_id JOIN agent_domain_outbox d ON d.event_id=NEW.event_id
    WHERE g.id=NEW.analysis_grant_id AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.resource_type='agent_context'
     AND g.actions=ARRAY['analyze_local']::text[] AND g.owner_account_id=a.id AND g.recipient_account_id=a.id
     AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp() AND g.expires_at>=NEW.expires_at
     AND p.id=NEW.analysis_preview_id AND p.id::text=g.resource_id
     AND p.owner_id=NEW.owner_id AND p.agent_id=NEW.agent_id AND p.session_id=NEW.session_id
     AND a.status='active' AND a.account_type='person' AND ag.status='active' AND ag.agent_type='personal' AND ag.principal_account_id=a.id
     AND se.account_id=a.id AND se.revoked_at IS NULL AND se.expires_at>=NEW.expires_at AND se.idle_expires_at>=NEW.expires_at
     AND t.owner_account_id=a.id AND t.acting_user_account_id=a.id AND t.principal_type='person' AND t.status='ACTIVE' AND t.context_type='CITY'
     AND m.author_account_id=a.id AND m.visibility='private' AND m.status='draft' AND m.revision=(p.selection->>'momentRevision')::bigint
     AND d.logical_operation_id=NEW.logical_operation_id AND d.source_id=m.id AND d.source_revision=m.revision
     AND d.source_status='draft' AND d.subject_id=a.id AND d.agent_id=ag.id AND d.expires_at>=NEW.expires_at
     AND NOT EXISTS(SELECT 1 FROM moment_activity_links WHERE moment_id=m.id)
     AND NOT EXISTS(SELECT 1 FROM moment_community_links WHERE moment_id=m.id)
     AND NOT EXISTS(SELECT 1 FROM moment_organization_links WHERE moment_id=m.id))
 THEN RAISE EXCEPTION 'retention preview needs exact current ordinary source and analysis grant' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_candidate_retention_preview_guard BEFORE INSERT OR UPDATE ON agent_candidate_retention_previews FOR EACH ROW EXECUTE FUNCTION birdtie_candidate_retention_preview_guard();
CREATE FUNCTION birdtie_candidate_retention_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' OR NOT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_candidate_retention_previews p ON p.id=NEW.preview_id
  WHERE g.id=NEW.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id
   AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='STAGE_MEMORY_CANDIDATE'
   AND g.actions=ARRAY['stage_candidate']::text[] AND g.revision=1 AND g.revoked_at IS NULL
   AND g.expires_at>clock_timestamp() AND g.expires_at<=p.expires_at)
 THEN RAISE EXCEPTION 'retention binding needs original exact grant' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_candidate_retention_binding_guard BEFORE INSERT OR UPDATE ON agent_candidate_retention_bindings FOR EACH ROW EXECUTE FUNCTION birdtie_candidate_retention_binding_guard();
CREATE FUNCTION birdtie_candidate_retention_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND NEW.purpose<>'STAGE_MEMORY_CANDIDATE' THEN RETURN NEW;END IF;
 IF TG_OP='UPDATE' AND OLD.purpose<>'STAGE_MEMORY_CANDIDATE' AND NEW.purpose<>'STAGE_MEMORY_CANDIDATE' THEN RETURN NEW;END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.resource_type<>'agent_context' OR NEW.actions<>ARRAY['stage_candidate']::text[] OR NEW.revision<>1 OR NEW.revoked_at IS NOT NULL
   OR NEW.owner_account_id IS DISTINCT FROM NEW.recipient_account_id OR NEW.expires_at IS NULL OR NOT isfinite(NEW.expires_at)
   OR NOT isfinite(NEW.created_at) OR NEW.created_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp() OR NEW.expires_at<=NEW.created_at
   OR NOT EXISTS(SELECT 1 FROM agent_candidate_retention_previews p JOIN consent_grants a ON a.id=p.analysis_grant_id
     JOIN sessions s ON s.id=p.session_id WHERE p.id::text=NEW.resource_id AND p.owner_id=NEW.owner_account_id
      AND p.expires_at>=NEW.expires_at AND a.purpose='MOMENT_LOCAL_ANALYSIS' AND a.actions=ARRAY['analyze_local']::text[]
      AND a.revoked_at IS NULL AND a.expires_at>=NEW.expires_at AND s.revoked_at IS NULL AND s.expires_at>=NEW.expires_at AND s.idle_expires_at>=NEW.expires_at)
  THEN RAISE EXCEPTION 'invalid independent candidate retention grant' USING ERRCODE='23514';END IF;
 ELSE
  IF OLD.purpose<>'STAGE_MEMORY_CANDIDATE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.owner_account_id IS DISTINCT FROM OLD.owner_account_id
   OR NEW.recipient_account_id IS DISTINCT FROM OLD.recipient_account_id OR NEW.resource_type IS DISTINCT FROM OLD.resource_type OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
   OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.actions IS DISTINCT FROM OLD.actions OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
   OR OLD.revoked_at IS NOT NULL OR OLD.revision=9223372036854775807 OR NEW.revision<>OLD.revision+1
   OR NEW.revoked_at IS NULL OR NOT isfinite(NEW.revoked_at) OR NEW.revoked_at<OLD.created_at OR NEW.revoked_at>clock_timestamp()
  THEN RAISE EXCEPTION 'retention grants permit only irreversible revoke' USING ERRCODE='23514';END IF;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_candidate_retention_grant_guard BEFORE INSERT OR UPDATE ON consent_grants FOR EACH ROW EXECUTE FUNCTION birdtie_candidate_retention_grant_guard();
COMMIT;
