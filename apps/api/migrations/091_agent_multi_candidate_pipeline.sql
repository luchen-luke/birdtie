BEGIN;
-- Metadata only until independent exact retention approval. The original
-- consent_grants retains the sole authorization lifecycle.
CREATE TABLE agent_multi_candidate_previews(
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id),
 agent_id uuid NOT NULL,
 owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 session_id uuid NOT NULL REFERENCES sessions(id),
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
CREATE INDEX agent_multi_candidate_previews_owner ON agent_multi_candidate_previews(owner_id,observed_at);
CREATE TABLE agent_multi_candidate_bindings(
 grant_id uuid PRIMARY KEY REFERENCES consent_grants(id),
 preview_id uuid NOT NULL UNIQUE REFERENCES agent_multi_candidate_previews(id)
);
CREATE FUNCTION birdtie_multi_candidate_sources_current(sel jsonb,own uuid,agent uuid,sid uuid,auth text) RETURNS boolean LANGUAGE sql VOLATILE SET TimeZone='UTC' AS $$
 WITH n AS MATERIALIZED(SELECT clock_timestamp() at), ids AS (SELECT value id FROM jsonb_array_elements_text(sel->'analysisGrantIds')),
 good AS (SELECT ep.moment_id,ep.task_id,g.id FROM ids x
 JOIN consent_grants g ON g.id=(x.id)::uuid
 JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id
 JOIN agent_enrichment_purpose_previews ep ON ep.id=eb.preview_id AND g.resource_id=ep.id::text
 JOIN accounts a ON a.id=own JOIN agents ag ON ag.id=agent AND ag.principal_account_id=a.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions se ON se.id=sid AND se.account_id=a.id
 JOIN agent_tasks t ON t.id=ep.task_id AND t.owner_account_id=a.id
 JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=t.city_context_id
 JOIN cities c ON c.id=cx.city_id JOIN city_contexts cc ON cc.city_id=c.id
 JOIN moments m ON m.id=ep.moment_id AND m.author_account_id=a.id
 CROSS JOIN n
 WHERE ep.owner_id=own AND ep.agent_id=agent AND ep.session_id=sid
 AND g.owner_account_id=a.id AND g.recipient_account_id=a.id AND g.resource_type='agent_context' AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.actions=ARRAY['analyze_local']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.created_at<=n.at AND g.expires_at>n.at
 AND ep.owner_id=a.id AND ep.agent_id=ag.id AND ep.session_id=se.id AND ep.session_id=sid AND ep.expires_at>n.at
 AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND ag.status='active' AND ag.created_at<=n.at AND ap.profile_version>0 AND ap.updated_at<=n.at
 AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at
 AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.context_type='CITY' AND t.status='ACTIVE' AND t.updated_at<=n.at
 AND COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query)<>''
 AND c.publication_status='published' AND cc.status='active' AND(c.expires_at IS NULL OR c.expires_at>n.at)
 AND m.visibility='private' AND m.status='draft' AND m.revision=(ep.selection->>'momentRevision')::bigint AND m.updated_at<=n.at AND m.updated_at+interval '15 minutes'>n.at
 AND NOT EXISTS(SELECT 1 FROM moment_activity_links WHERE moment_id=m.id) AND NOT EXISTS(SELECT 1 FROM moment_community_links WHERE moment_id=m.id) AND NOT EXISTS(SELECT 1 FROM moment_organization_links WHERE moment_id=m.id)
 AND ep.authority=auth AND ep.authority=encode(sha256(convert_to(jsonb_build_object('owner',a.id,'ownerRow',a.xmin::text,'agent',ag.id,'agentRow',ag.xmin::text,
 'metadataRow',ap.xmin::text,'metadataVersion',ap.profile_version,'session',se.id,'created',se.created_at,'method',se.authentication_method,'absoluteExpiry',se.expires_at,'digest',encode(se.token_sha256,'hex'))::text,'UTF8')),'hex')
 AND ep.source_binding=encode(sha256(convert_to(jsonb_build_object('moment',m.id,'row',m.xmin::text,'revision',m.revision,'updated',m.updated_at,
 'selected',(CASE WHEN ep.selection->'fields' ? 'title' THEN jsonb_build_object('title',m.title) ELSE '{}'::jsonb END)||(CASE WHEN ep.selection->'fields' ? 'body' THEN jsonb_build_object('body',m.body) ELSE '{}'::jsonb END))::text,'UTF8')),'hex')
 AND ep.task_binding=encode(sha256(convert_to(jsonb_build_object('task',t.id,'row',t.xmin::text,'updated',t.updated_at,'context',cx.id,'contextRow',cx.xmin::text,
 'cityRow',c.xmin::text,'cityStateRow',cc.xmin::text,'query',encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex'))::text,'UTF8')),'hex'))
 SELECT jsonb_typeof(sel->'analysisGrantIds')='array' AND jsonb_array_length(sel->'analysisGrantIds') BETWEEN 2 AND 5
 AND (SELECT count(*) FROM good)=jsonb_array_length(sel->'analysisGrantIds')
 AND (SELECT count(DISTINCT id) FROM good)=jsonb_array_length(sel->'analysisGrantIds')
 AND (SELECT count(DISTINCT moment_id) FROM good)=jsonb_array_length(sel->'analysisGrantIds')
 AND (SELECT count(DISTINCT task_id) FROM good)=1
 $$;
CREATE OR REPLACE FUNCTION birdtie_candidate_pipeline_single_current(gid uuid) RETURNS boolean LANGUAGE sql VOLATILE SET TimeZone='UTC' AS $$
 WITH n AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM consent_grants rg
 JOIN agent_candidate_retention_bindings rb ON rb.grant_id=rg.id
 JOIN agent_candidate_retention_previews rp ON rp.id=rb.preview_id AND rg.resource_id=rp.id::text
 JOIN consent_grants g ON g.id=rp.analysis_grant_id
 JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id
 JOIN agent_enrichment_purpose_previews ep ON ep.id=eb.preview_id AND ep.id=rp.analysis_preview_id AND g.resource_id=ep.id::text
 JOIN accounts a ON a.id=rp.owner_id JOIN agents ag ON ag.id=rp.agent_id AND ag.principal_account_id=a.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions se ON se.id=rp.session_id AND se.account_id=a.id
 JOIN agent_tasks t ON t.id=ep.task_id AND t.owner_account_id=a.id
 JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=t.city_context_id
 JOIN cities c ON c.id=cx.city_id JOIN city_contexts cc ON cc.city_id=c.id
 JOIN moments m ON m.id=ep.moment_id AND m.author_account_id=a.id
 JOIN agent_domain_outbox d ON d.event_id=rp.event_id CROSS JOIN n
 WHERE rg.id=gid AND rg.owner_account_id=a.id AND rg.recipient_account_id=a.id AND rg.resource_type='agent_context'
 AND rg.purpose='STAGE_MEMORY_CANDIDATE' AND rg.actions=ARRAY['stage_candidate']::text[] AND rg.revision=1 AND rg.revoked_at IS NULL AND rg.created_at<=n.at AND rg.expires_at>n.at AND rg.expires_at<=rp.expires_at
 AND g.owner_account_id=a.id AND g.recipient_account_id=a.id AND g.resource_type='agent_context' AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.actions=ARRAY['analyze_local']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.created_at<=n.at AND g.expires_at>n.at
 AND ep.owner_id=a.id AND ep.agent_id=ag.id AND ep.session_id=se.id AND ep.session_id=rp.session_id AND ep.expires_at>n.at AND rp.expires_at>n.at
 AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND ag.status='active' AND ag.created_at<=n.at AND ap.profile_version>0 AND ap.updated_at<=n.at
 AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at
 AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.context_type='CITY' AND t.status='ACTIVE' AND t.updated_at<=n.at
 AND COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query)<>''
 AND c.publication_status='published' AND cc.status='active' AND(c.expires_at IS NULL OR c.expires_at>n.at)
 AND m.visibility='private' AND m.status='draft' AND m.revision=(ep.selection->>'momentRevision')::bigint AND m.updated_at<=n.at AND m.updated_at+interval '15 minutes'>n.at
 AND NOT EXISTS(SELECT 1 FROM moment_activity_links WHERE moment_id=m.id) AND NOT EXISTS(SELECT 1 FROM moment_community_links WHERE moment_id=m.id) AND NOT EXISTS(SELECT 1 FROM moment_organization_links WHERE moment_id=m.id)
 AND d.subject_id=a.id AND d.agent_id=ag.id AND d.source_id=m.id AND d.source_revision=m.revision AND d.source_status='draft' AND d.logical_operation_id=rp.logical_operation_id AND d.expires_at>n.at
 AND ep.authority=rp.authority AND ep.authority=encode(sha256(convert_to(jsonb_build_object('owner',a.id,'ownerRow',a.xmin::text,'agent',ag.id,'agentRow',ag.xmin::text,
 'metadataRow',ap.xmin::text,'metadataVersion',ap.profile_version,'session',se.id,'created',se.created_at,'method',se.authentication_method,'absoluteExpiry',se.expires_at,'digest',encode(se.token_sha256,'hex'))::text,'UTF8')),'hex')
 AND ep.source_binding=encode(sha256(convert_to(jsonb_build_object('moment',m.id,'row',m.xmin::text,'revision',m.revision,'updated',m.updated_at,
 'selected',(CASE WHEN ep.selection->'fields' ? 'title' THEN jsonb_build_object('title',m.title) ELSE '{}'::jsonb END)||(CASE WHEN ep.selection->'fields' ? 'body' THEN jsonb_build_object('body',m.body) ELSE '{}'::jsonb END))::text,'UTF8')),'hex')
 AND ep.task_binding=encode(sha256(convert_to(jsonb_build_object('task',t.id,'row',t.xmin::text,'updated',t.updated_at,'context',cx.id,'contextRow',cx.xmin::text,
 'cityRow',c.xmin::text,'cityStateRow',cc.xmin::text,'query',encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex'))::text,'UTF8')),'hex'))
 $$;
CREATE FUNCTION birdtie_multi_candidate_preview_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE SET TimeZone='UTC' AS $$
 WITH n AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM agent_multi_candidate_previews p CROSS JOIN n
 JOIN agent_domain_outbox d ON d.event_id=p.event_id
 JOIN agent_enrichment_purpose_previews ep ON ep.moment_id=d.source_id
 JOIN agent_enrichment_purpose_bindings eb ON eb.preview_id=ep.id
 WHERE p.id=pid AND p.expires_at>n.at AND p.observed_at<=n.at
 AND birdtie_multi_candidate_sources_current(p.selection,p.owner_id,p.agent_id,p.session_id,p.authority)
 AND p.selection->'analysisGrantIds' ? eb.grant_id::text AND ep.owner_id=p.owner_id AND ep.agent_id=p.agent_id AND ep.session_id=p.session_id
 AND ep.moment_id=(SELECT min(e.moment_id::text)::uuid FROM agent_enrichment_purpose_previews e JOIN agent_enrichment_purpose_bindings b ON b.preview_id=e.id WHERE p.selection->'analysisGrantIds' ? b.grant_id::text)
 AND d.subject_id=p.owner_id AND d.agent_id=p.agent_id AND d.logical_operation_id=p.logical_operation_id AND d.source_status='draft'
 AND d.source_revision=(ep.selection->>'momentRevision')::bigint AND d.expires_at>=p.expires_at
 AND NOT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_enrichment_purpose_bindings b ON b.grant_id=g.id JOIN agent_enrichment_purpose_previews e ON e.id=b.preview_id JOIN sessions s ON s.id=e.session_id JOIN moments m ON m.id=e.moment_id JOIN agent_tasks t ON t.id=e.task_id JOIN cities c ON c.id=t.city_context_id
 WHERE p.selection->'analysisGrantIds' ? g.id::text AND (g.expires_at<p.expires_at OR e.expires_at<p.expires_at OR s.expires_at<p.expires_at OR s.idle_expires_at<p.expires_at OR m.updated_at+interval '15 minutes'<p.expires_at OR (c.expires_at IS NOT NULL AND c.expires_at<p.expires_at))))
 $$;
CREATE OR REPLACE FUNCTION birdtie_candidate_pipeline_current(gid uuid) RETURNS boolean LANGUAGE sql VOLATILE SET TimeZone='UTC' AS $$
 SELECT birdtie_candidate_pipeline_single_current(gid) OR EXISTS(SELECT 1 FROM consent_grants g JOIN agent_multi_candidate_bindings b ON b.grant_id=g.id JOIN agent_multi_candidate_previews p ON p.id=b.preview_id
 WHERE g.id=gid AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='STAGE_MEMORY_CANDIDATE_MULTI' AND g.actions=ARRAY['stage_candidate']::text[] AND g.revision=1 AND g.revoked_at IS NULL AND g.created_at<=clock_timestamp() AND g.expires_at>clock_timestamp() AND g.expires_at<=p.expires_at AND birdtie_multi_candidate_preview_current(p.id)) $$;
CREATE FUNCTION birdtie_multi_candidate_preview_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' OR (SELECT count(*) FROM jsonb_object_keys(NEW.selection))<>2 OR NOT(NEW.selection ?& ARRAY['analysisGrantIds','retainUntil']) OR NEW.expires_at>(NEW.selection->>'retainUntil')::timestamptz OR NOT birdtie_multi_candidate_sources_current(NEW.selection,NEW.owner_id,NEW.agent_id,NEW.session_id,NEW.authority)
 OR NOT EXISTS(SELECT 1 FROM agent_domain_outbox d JOIN agent_enrichment_purpose_previews p ON p.moment_id=d.source_id JOIN agent_enrichment_purpose_bindings b ON b.preview_id=p.id WHERE d.event_id=NEW.event_id AND NEW.selection->'analysisGrantIds' ? b.grant_id::text AND d.subject_id=NEW.owner_id AND d.agent_id=NEW.agent_id AND d.logical_operation_id=NEW.logical_operation_id AND d.source_revision=(p.selection->>'momentRevision')::bigint AND d.expires_at>=NEW.expires_at)
 THEN RAISE EXCEPTION 'multi retention preview requires current explicit source selection' USING ERRCODE='23514';END IF; RETURN NEW;
END $$;
CREATE TRIGGER agent_multi_candidate_preview_guard BEFORE INSERT OR UPDATE ON agent_multi_candidate_previews FOR EACH ROW EXECUTE FUNCTION birdtie_multi_candidate_preview_guard();
CREATE FUNCTION birdtie_multi_candidate_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' OR NOT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_multi_candidate_previews p ON p.id=NEW.preview_id
  WHERE g.id=NEW.grant_id AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id
   AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='STAGE_MEMORY_CANDIDATE_MULTI'
   AND g.actions=ARRAY['stage_candidate']::text[] AND g.revision=1 AND g.revoked_at IS NULL
   AND g.expires_at>clock_timestamp() AND g.expires_at<=p.expires_at)
 THEN RAISE EXCEPTION 'retention binding needs original exact grant' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_multi_candidate_binding_guard BEFORE INSERT OR UPDATE ON agent_multi_candidate_bindings FOR EACH ROW EXECUTE FUNCTION birdtie_multi_candidate_binding_guard();
CREATE FUNCTION birdtie_multi_candidate_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND NEW.purpose<>'STAGE_MEMORY_CANDIDATE_MULTI' THEN RETURN NEW;END IF;
 IF TG_OP='UPDATE' AND OLD.purpose<>'STAGE_MEMORY_CANDIDATE_MULTI' AND NEW.purpose<>'STAGE_MEMORY_CANDIDATE_MULTI' THEN RETURN NEW;END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.resource_type<>'agent_context' OR NEW.actions<>ARRAY['stage_candidate']::text[] OR NEW.revision<>1 OR NEW.revoked_at IS NOT NULL
   OR NEW.owner_account_id IS DISTINCT FROM NEW.recipient_account_id OR NEW.expires_at IS NULL OR NOT isfinite(NEW.expires_at)
   OR NOT isfinite(NEW.created_at) OR NEW.created_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp() OR NEW.expires_at<=NEW.created_at
   OR NOT EXISTS(SELECT 1 FROM agent_multi_candidate_previews p WHERE p.id::text=NEW.resource_id AND p.owner_id=NEW.owner_account_id AND p.expires_at>=NEW.expires_at AND birdtie_multi_candidate_preview_current(p.id))
  THEN RAISE EXCEPTION 'invalid independent candidate retention grant' USING ERRCODE='23514';END IF;
 ELSE
  IF OLD.purpose<>'STAGE_MEMORY_CANDIDATE_MULTI' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.owner_account_id IS DISTINCT FROM OLD.owner_account_id
   OR NEW.recipient_account_id IS DISTINCT FROM OLD.recipient_account_id OR NEW.resource_type IS DISTINCT FROM OLD.resource_type OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
   OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.actions IS DISTINCT FROM OLD.actions OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
   OR OLD.revoked_at IS NOT NULL OR OLD.revision=9223372036854775807 OR NEW.revision<>OLD.revision+1
   OR NEW.revoked_at IS NULL OR NOT isfinite(NEW.revoked_at) OR NEW.revoked_at<OLD.created_at OR NEW.revoked_at>clock_timestamp()
  THEN RAISE EXCEPTION 'retention grants permit only irreversible revoke' USING ERRCODE='23514';END IF;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER agent_multi_candidate_grant_guard BEFORE INSERT OR UPDATE ON consent_grants FOR EACH ROW EXECUTE FUNCTION birdtie_multi_candidate_grant_guard();

ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_handler_version_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_handler_version_check CHECK(handler_version IN('mom-control-v1','mom-control-v2','mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_check1;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_check1 CHECK((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED') OR(control_state='CANDIDATE_STAGED' AND reason_code='CANDIDATE_STAGED' AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1')));
ALTER TABLE agent_effect_ledger DROP CONSTRAINT candidate_effect_native_binding;
ALTER TABLE agent_effect_ledger ADD CONSTRAINT candidate_effect_native_binding CHECK(candidate_id IS NOT NULL AND retention_grant_id IS NOT NULL AND event_id IS NOT NULL AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1') AND fence>0 AND attempt BETWEEN 1 AND 20 AND attempt<=fence);

CREATE FUNCTION birdtie_multi_candidate_effect_validate(e agent_effect_ledger) RETURNS void LANGUAGE plpgsql SET TimeZone='UTC' AS $$
DECLARE p agent_multi_candidate_previews%ROWTYPE; c agent_memory_candidates%ROWTYPE; d agent_domain_outbox%ROWTYPE; i agent_consumer_inbox%ROWTYPE; r jsonb; key text; epoch text;
BEGIN
 IF e.handler_version IS DISTINCT FROM 'mom-candidate-multi-v1' OR e.candidate_id IS NULL OR e.retention_grant_id IS NULL OR e.event_id IS NULL OR e.proof_review IS NULL OR octet_length(e.proof_review)>32768 THEN RAISE EXCEPTION 'multi effect requires bounded native proof' USING ERRCODE='55000';END IF;
 SELECT mp.* INTO p FROM agent_multi_candidate_previews mp JOIN agent_multi_candidate_bindings b ON b.preview_id=mp.id WHERE b.grant_id=e.retention_grant_id;
 SELECT * INTO c FROM agent_memory_candidates WHERE id=e.candidate_id;
 SELECT * INTO d FROM agent_domain_outbox WHERE event_id=e.event_id;
 SELECT * INTO i FROM agent_consumer_inbox WHERE event_id=e.event_id AND subject_id=e.subject_id AND handler_version=e.handler_version;
 epoch:=(txid_current()%4294967296)::text;
 IF p.id IS NULL OR c.id IS NULL OR d.event_id IS NULL OR i.event_id IS NULL OR NOT birdtie_candidate_pipeline_current(e.retention_grant_id)
 OR (SELECT xmin::text FROM agent_memory_candidates WHERE id=c.id) IS DISTINCT FROM epoch OR (SELECT xmin::text FROM agent_domain_outbox WHERE event_id=d.event_id) IS DISTINCT FROM epoch OR (SELECT xmin::text FROM agent_consumer_inbox WHERE event_id=i.event_id AND subject_id=i.subject_id AND handler_version=i.handler_version) IS DISTINCT FROM epoch
 OR c.owner_id<>e.subject_id OR c.agent_id<>e.agent_id OR c.owner_id<>p.owner_id OR c.agent_id<>p.agent_id OR c.status<>'CANDIDATE' OR c.version<>1 OR c.memory_id IS NOT NULL OR c.decision_digest IS NOT NULL
 OR c.valid_until<=clock_timestamp() OR c.valid_until>p.expires_at OR c.valid_until>d.lease_until OR c.valid_until>(SELECT expires_at FROM consent_grants WHERE id=e.retention_grant_id)
 OR c.metadata_version<>(SELECT profile_version FROM agent_profiles WHERE agent_id=e.agent_id AND owner_id=e.subject_id AND owner_type='PERSON')
 OR c.created_at<p.observed_at OR c.created_at<(SELECT created_at FROM consent_grants WHERE id=e.retention_grant_id) OR c.created_at>clock_timestamp()
 OR e.event_id<>p.event_id OR e.logical_operation_id<>p.logical_operation_id OR e.logical_operation_id<>d.logical_operation_id OR e.action_id<>'519fde3b-cc12-4abc-8c4f-f3227290a815' OR e.effect_kind<>'MEMORY_CANDIDATE'
 OR e.source_id<>d.source_id OR e.source_revision<>d.source_revision OR d.subject_id<>e.subject_id OR d.agent_id<>e.agent_id OR e.fence<>d.fence OR e.attempt<>d.attempt OR i.fence<>d.fence OR i.attempt<>d.attempt OR i.control_state<>'LEASED' OR d.delivery_state<>'LEASED' OR d.lease_until<=clock_timestamp()
 OR e.action_digest<>p.review_digest OR encode(sha256(convert_to(e.proof_review,'UTF8')),'hex')<>p.review_digest THEN RAISE EXCEPTION 'multi native current effect binding invalid' USING ERRCODE='55000';END IF;
 r:=e.proof_review::jsonb;
 -- SQL NULL is not approval. Require scalar/collection types before comparisons
 -- or casts; a JSON null member must not disappear through three-valued logic.
 IF jsonb_typeof(r) IS DISTINCT FROM 'object'
 OR EXISTS(SELECT 1 FROM unnest(ARRAY['anchorEventId','logicalOperationId','taskId','retainUntil']) k WHERE jsonb_typeof(r->k) IS DISTINCT FROM 'string')
 OR jsonb_typeof(r->'clusters') IS DISTINCT FROM 'number'
 OR jsonb_typeof(r->'proposal') IS DISTINCT FROM 'object'
 OR jsonb_typeof(r->'anchorSource') IS DISTINCT FROM 'object'
 OR jsonb_typeof(r->'sources') IS DISTINCT FROM 'array'
 OR jsonb_typeof(r->'sourceSelections') IS DISTINCT FROM 'array' THEN
 RAISE EXCEPTION 'multi proof requires exact nonnull typed fields' USING ERRCODE='55000';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(r->'sourceSelections') s
 WHERE jsonb_typeof(s) IS DISTINCT FROM 'object'
 OR jsonb_typeof(s->'analysisGrantId') IS DISTINCT FROM 'string'
 OR jsonb_typeof(s->'analysisPreviewId') IS DISTINCT FROM 'string'
 OR jsonb_typeof(s->'source') IS DISTINCT FROM 'object'
 OR jsonb_typeof(s->'selectedFields') IS DISTINCT FROM 'array') THEN
 RAISE EXCEPTION 'multi members require exact nonnull fields' USING ERRCODE='55000';END IF;
 IF jsonb_typeof(r)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(r))<>9 OR NOT(r ?& ARRAY['proposal','sources','sourceSelections','anchorEventId','logicalOperationId','anchorSource','taskId','retainUntil','clusters'])
 OR r->>'anchorEventId' IS DISTINCT FROM e.event_id::text OR r->>'logicalOperationId' IS DISTINCT FROM e.logical_operation_id::text OR r->'anchorSource' IS DISTINCT FROM r->'sources'->0
 OR r->'anchorSource'->'selector'->>'id' IS DISTINCT FROM e.source_id::text OR (r->'anchorSource'->'version'->>'revision')::bigint IS DISTINCT FROM e.source_revision
 OR (r->>'retainUntil')::timestamptz<>p.expires_at OR jsonb_array_length(r->'sources') NOT BETWEEN 2 AND 5 OR jsonb_array_length(r->'sourceSelections')<>jsonb_array_length(r->'sources') OR jsonb_array_length(r->'sources')<>jsonb_array_length(p.selection->'analysisGrantIds')
 OR (r->>'clusters')::integer<>jsonb_array_length(r->'sources') OR c.sources IS DISTINCT FROM r->'sources'
 OR r->'proposal'->>'algorithmVersion' IS DISTINCT FROM 'moment-lexical-category-v1' OR r->'proposal'->>'predicate' IS DISTINCT FROM c.predicate OR r->'proposal'->>'category' IS DISTINCT FROM c.category OR r->'proposal'->'assessment' IS DISTINCT FROM c.assessment OR c.assessment<>'{"semantics":"ORDINAL","level":"LOW"}'::jsonb
 OR c.intent_digest<>encode(sha256(convert_to('{"Predicate":'||to_json(c.predicate)::text||',"Category":'||to_json(c.category)::text||',"Sources":'||json_extract_path(e.proof_review::json,'sources')::text||'}','UTF8')),'hex')
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(r->'sourceSelections') WITH ORDINALITY z(s,ord)
 LEFT JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=(s->>'analysisGrantId')::uuid
 LEFT JOIN agent_enrichment_purpose_previews ep ON ep.id=eb.preview_id
 WHERE ep.id IS NULL OR NOT(p.selection->'analysisGrantIds' ? (s->>'analysisGrantId')) OR (SELECT count(*) FROM jsonb_object_keys(s))<>4 OR NOT(s ?& ARRAY['analysisGrantId','analysisPreviewId','source','selectedFields'])
 OR s->>'analysisPreviewId' IS DISTINCT FROM ep.id::text OR s->'selectedFields' IS DISTINCT FROM ep.selection->'fields' OR s->'source' IS DISTINCT FROM r->'sources'->((ord-1)::integer)
 OR ep.task_id::text IS DISTINCT FROM r->>'taskId' OR s->'source'->'selector'->>'type' IS DISTINCT FROM 'MOMENT' OR s->'source'->'selector'->>'id' IS DISTINCT FROM ep.moment_id::text OR s->'source'->'version'->>'kind' IS DISTINCT FROM 'REVISION' OR (s->'source'->'version'->>'revision')::bigint IS DISTINCT FROM (ep.selection->>'momentRevision')::bigint OR s->'source'->'anchors' IS DISTINCT FROM jsonb_build_array('MOMENT:'||ep.moment_id::text)
 OR (s->'source'->>'eventTime')::timestamptz IS DISTINCT FROM (SELECT updated_at FROM moments WHERE id=ep.moment_id))
 OR (SELECT count(DISTINCT s->>'analysisGrantId') FROM jsonb_array_elements(r->'sourceSelections') s)<>jsonb_array_length(r->'sourceSelections')
 OR (SELECT count(DISTINCT s->'selector'->>'id') FROM jsonb_array_elements(r->'sources') s)<>jsonb_array_length(r->'sources')
 THEN RAISE EXCEPTION 'multi effect must match every approved source and exact structured proposal' USING ERRCODE='55000';END IF;
 key:=encode(sha256(convert_to('birdtie.outbox.effect-address.v1','UTF8')||decode('00','hex')||convert_to('{"Tenant":{"type":"PERSON","id":"'||e.subject_id::text||'"},"Subject":{"type":"PERSON","id":"'||e.subject_id::text||'"},"AgentID":"'||e.agent_id::text||'","LogicalOperationID":"'||e.logical_operation_id::text||'","ActionID":"'||e.action_id::text||'","Kind":"MEMORY_CANDIDATE"}','UTF8')),'hex');
 IF key<>e.effect_key THEN RAISE EXCEPTION 'multi stable effect address mismatch' USING ERRCODE='55000';END IF;
END $$;
CREATE FUNCTION birdtie_multi_candidate_human_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='CANDIDATE' AND NEW.status='ACTIVE' AND EXISTS(SELECT 1 FROM agent_effect_ledger e WHERE e.candidate_id=OLD.id AND e.handler_version='mom-candidate-multi-v1' AND NOT birdtie_candidate_pipeline_current(e.retention_grant_id)) THEN RAISE EXCEPTION 'multi candidate original approvals no longer current' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_multi_candidate_human_decision_guard BEFORE UPDATE ON agent_memory_candidates FOR EACH ROW EXECUTE FUNCTION birdtie_multi_candidate_human_decision_guard();

CREATE OR REPLACE FUNCTION birdtie_agent_effect_writer_unavailable() RETURNS trigger LANGUAGE plpgsql SET TimeZone='UTC' AS $$
DECLARE p agent_candidate_retention_previews%ROWTYPE; c agent_memory_candidates%ROWTYPE;
 d agent_domain_outbox%ROWTYPE; i agent_consumer_inbox%ROWTYPE; r jsonb; key text; candidate_epoch text; event_epoch text; inbox_epoch text;
BEGIN
 IF TG_OP='INSERT' AND NEW.handler_version='mom-candidate-multi-v1' THEN
  PERFORM birdtie_multi_candidate_effect_validate(NEW);NEW.proof_review:=NULL;NEW.created_at:=clock_timestamp();RETURN NEW;
 END IF;
 IF TG_OP<>'INSERT' OR NEW.candidate_id IS NULL OR NEW.retention_grant_id IS NULL OR NEW.event_id IS NULL OR NEW.proof_review IS NULL THEN
  RAISE EXCEPTION 'actual candidate effect writer requires native proof' USING ERRCODE='55000'; END IF;
 SELECT rp.* INTO p FROM agent_candidate_retention_previews rp JOIN agent_candidate_retention_bindings b ON b.preview_id=rp.id WHERE b.grant_id=NEW.retention_grant_id;
 SELECT * INTO c FROM agent_memory_candidates WHERE id=NEW.candidate_id; SELECT xmin::text INTO candidate_epoch FROM agent_memory_candidates WHERE id=NEW.candidate_id;
 SELECT * INTO d FROM agent_domain_outbox WHERE event_id=NEW.event_id; SELECT xmin::text INTO event_epoch FROM agent_domain_outbox WHERE event_id=NEW.event_id;
 SELECT * INTO i FROM agent_consumer_inbox WHERE event_id=NEW.event_id AND subject_id=NEW.subject_id AND handler_version=NEW.handler_version; SELECT xmin::text INTO inbox_epoch FROM agent_consumer_inbox WHERE event_id=NEW.event_id AND subject_id=NEW.subject_id AND handler_version=NEW.handler_version;
 IF p.id IS NULL OR c.id IS NULL OR d.event_id IS NULL OR i.event_id IS NULL OR NOT birdtie_candidate_pipeline_current(NEW.retention_grant_id)
 OR candidate_epoch IS DISTINCT FROM (txid_current()%4294967296)::text OR event_epoch IS DISTINCT FROM (txid_current()%4294967296)::text OR inbox_epoch IS DISTINCT FROM (txid_current()%4294967296)::text
 OR c.owner_id<>NEW.subject_id OR c.agent_id<>NEW.agent_id OR c.owner_id<>p.owner_id OR c.agent_id<>p.agent_id
 OR c.status<>'CANDIDATE' OR c.version<>1 OR c.memory_id IS NOT NULL OR c.decision_digest IS NOT NULL OR c.valid_until<=clock_timestamp()
 OR c.metadata_version<>(SELECT profile_version FROM agent_profiles WHERE agent_id=NEW.agent_id AND owner_id=NEW.subject_id AND owner_type='PERSON')
 OR c.created_at<p.observed_at OR c.created_at<(SELECT created_at FROM consent_grants WHERE id=NEW.retention_grant_id) OR c.created_at>clock_timestamp()
 OR c.valid_until>p.expires_at OR c.valid_until>(SELECT expires_at FROM consent_grants WHERE id=NEW.retention_grant_id)
 OR NEW.event_id<>p.event_id OR NEW.logical_operation_id<>p.logical_operation_id OR NEW.logical_operation_id<>d.logical_operation_id
 OR NEW.action_id<>'519fde3b-cc12-4abc-8c4f-f3227290a815' OR NEW.effect_kind<>'MEMORY_CANDIDATE'
 OR NEW.source_id<>d.source_id OR NEW.source_revision<>d.source_revision OR d.subject_id<>NEW.subject_id OR d.agent_id<>NEW.agent_id
 OR NEW.handler_version NOT IN('mom-candidate-local-v1','mom-candidate-local-v2') OR NEW.fence<>d.fence OR NEW.attempt<>d.attempt
 OR i.control_state<>'LEASED' OR i.fence<>d.fence OR i.attempt<>d.attempt OR d.delivery_state<>'LEASED' OR d.lease_until<=clock_timestamp()
 OR c.valid_until>d.lease_until OR NEW.action_digest<>p.review_digest OR octet_length(NEW.proof_review)>8192
 OR encode(sha256(convert_to(NEW.proof_review,'UTF8')),'hex')<>p.review_digest THEN
  RAISE EXCEPTION 'candidate effect proof or current authority invalid' USING ERRCODE='55000'; END IF;
 r:=NEW.proof_review::jsonb;
 IF jsonb_typeof(r)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(r))<>6 OR NOT(r ?& ARRAY['proposal','source','taskId','selectedFields','retainUntil','clusters'])
 OR r->>'taskId'<>(SELECT selection->>'taskId' FROM agent_enrichment_purpose_previews WHERE id=p.analysis_preview_id)
 OR r->'selectedFields'<>(SELECT selection->'fields' FROM agent_enrichment_purpose_previews WHERE id=p.analysis_preview_id)
 OR (r->>'retainUntil')::timestamptz<>p.expires_at OR r->'clusters'<>'1'::jsonb
 OR r->'proposal'->>'algorithmVersion'<>'moment-lexical-category-v1' OR r->'proposal'->>'predicate' IS DISTINCT FROM c.predicate
 OR r->'proposal'->>'category' IS DISTINCT FROM c.category OR r->'proposal'->'assessment' IS DISTINCT FROM c.assessment
 OR c.assessment<>'{"semantics":"ORDINAL","level":"LOW"}'::jsonb
 OR c.intent_digest<>encode(sha256(convert_to('{"Predicate":'||to_json(c.predicate)::text||',"Category":'||to_json(c.category)::text||',"Sources":['||json_extract_path(NEW.proof_review::json,'source')::text||']}','UTF8')),'hex')
 OR c.sources IS DISTINCT FROM jsonb_build_array(r->'source') OR r->'source'->'selector'->>'type'<>'MOMENT'
 OR r->'source'->'selector'->>'id'<>NEW.source_id::text OR (r->'source'->'version'->>'revision')::bigint<>NEW.source_revision
 OR r->'source'->'anchors'<>jsonb_build_array('MOMENT:'||NEW.source_id::text)
 THEN RAISE EXCEPTION 'effect must stage exactly the approved structured candidate' USING ERRCODE='55000'; END IF;
 key:=encode(sha256(convert_to('birdtie.outbox.effect-address.v1','UTF8')||decode('00','hex')||convert_to(
 '{"Tenant":{"type":"PERSON","id":"'||NEW.subject_id::text||'"},"Subject":{"type":"PERSON","id":"'||NEW.subject_id::text||'"},"AgentID":"'||NEW.agent_id::text||'","LogicalOperationID":"'||NEW.logical_operation_id::text||'","ActionID":"'||NEW.action_id::text||'","Kind":"MEMORY_CANDIDATE"}','UTF8')),'hex');
 IF key<>NEW.effect_key THEN RAISE EXCEPTION 'stable effect address mismatch' USING ERRCODE='55000'; END IF;
 NEW.proof_review:=NULL; NEW.created_at:=clock_timestamp(); RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_candidate_pipeline_revoke() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL
 FROM agent_effect_ledger e LEFT JOIN agent_candidate_retention_bindings rb ON rb.grant_id=e.retention_grant_id LEFT JOIN agent_candidate_retention_previews rp ON rp.id=rb.preview_id
 WHERE c.id=e.candidate_id AND c.status='CANDIDATE' AND(NEW.id=e.retention_grant_id OR NEW.id=rp.analysis_grant_id);
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL
 FROM agent_effect_ledger e JOIN agent_multi_candidate_bindings b ON b.grant_id=e.retention_grant_id JOIN agent_multi_candidate_previews p ON p.id=b.preview_id
 WHERE c.id=e.candidate_id AND c.status='CANDIDATE' AND(e.retention_grant_id=NEW.id OR p.selection->'analysisGrantIds' ? NEW.id::text);
 END IF; RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_expire_candidate_pipeline(batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE total integer; more integer;
BEGIN
 IF batch_size<1 OR batch_size>100 THEN RAISE EXCEPTION 'bounded candidate cleanup required'; END IF;
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id
 JOIN consent_grants g ON g.id=e.retention_grant_id JOIN agent_candidate_retention_bindings rb ON rb.grant_id=g.id JOIN agent_candidate_retention_previews rp ON rp.id=rb.preview_id JOIN consent_grants ag ON ag.id=rp.analysis_grant_id
 WHERE c.status='CANDIDATE' AND(c.valid_until<=clock_timestamp() OR g.revoked_at IS NOT NULL OR g.expires_at<=clock_timestamp() OR ag.revoked_at IS NOT NULL OR ag.expires_at<=clock_timestamp())
 ORDER BY c.id LIMIT batch_size FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS total=ROW_COUNT;
 IF total<batch_size THEN
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id WHERE e.handler_version='mom-candidate-multi-v1' AND c.status='CANDIDATE' AND(c.valid_until<=clock_timestamp() OR NOT birdtie_candidate_pipeline_current(e.retention_grant_id)) ORDER BY c.id LIMIT batch_size-total FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS more=ROW_COUNT;total:=total+more; END IF; RETURN total;
END $$;
COMMIT;
