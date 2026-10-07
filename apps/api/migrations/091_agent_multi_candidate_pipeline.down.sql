BEGIN;
LOCK TABLE consent_grants,agent_multi_candidate_previews,agent_multi_candidate_bindings,agent_domain_outbox,agent_consumer_inbox,agent_effect_ledger,agent_memory_candidates IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_multi_candidate_bindings) OR EXISTS(SELECT 1 FROM consent_grants WHERE purpose='STAGE_MEMORY_CANDIDATE_MULTI') OR EXISTS(SELECT 1 FROM agent_effect_ledger WHERE handler_version='mom-candidate-multi-v1') OR EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE handler_version='mom-candidate-multi-v1') THEN RAISE EXCEPTION '091 multi candidate permission or effect history prevents down' USING ERRCODE='55000';END IF;
END $$;
DROP TRIGGER agent_multi_candidate_human_decision_guard ON agent_memory_candidates;
DROP FUNCTION birdtie_multi_candidate_human_decision_guard();
CREATE OR REPLACE FUNCTION birdtie_agent_effect_writer_unavailable() RETURNS trigger LANGUAGE plpgsql SET TimeZone='UTC' AS $$
DECLARE p agent_candidate_retention_previews%ROWTYPE; c agent_memory_candidates%ROWTYPE;
 d agent_domain_outbox%ROWTYPE; i agent_consumer_inbox%ROWTYPE; r jsonb; key text; candidate_epoch text; event_epoch text; inbox_epoch text;
BEGIN
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
 END IF; RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_expire_candidate_pipeline(batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE total integer;
BEGIN
 IF batch_size<1 OR batch_size>100 THEN RAISE EXCEPTION 'bounded candidate cleanup required'; END IF;
 WITH expired AS(SELECT c.id FROM agent_memory_candidates c JOIN agent_effect_ledger e ON e.candidate_id=c.id
 JOIN consent_grants g ON g.id=e.retention_grant_id JOIN agent_candidate_retention_bindings rb ON rb.grant_id=g.id JOIN agent_candidate_retention_previews rp ON rp.id=rb.preview_id JOIN consent_grants ag ON ag.id=rp.analysis_grant_id
 WHERE c.status='CANDIDATE' AND(c.valid_until<=clock_timestamp() OR g.revoked_at IS NOT NULL OR g.expires_at<=clock_timestamp() OR ag.revoked_at IS NOT NULL OR ag.expires_at<=clock_timestamp())
 ORDER BY c.id LIMIT batch_size FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET version=c.version+1,status='EXPIRED',predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM expired x WHERE c.id=x.id;
 GET DIAGNOSTICS total=ROW_COUNT; RETURN total;
END $$;
CREATE OR REPLACE FUNCTION birdtie_candidate_pipeline_current(gid uuid) RETURNS boolean LANGUAGE sql VOLATILE SET TimeZone='UTC' AS $$
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
DROP FUNCTION birdtie_multi_candidate_effect_validate(agent_effect_ledger);
DROP TRIGGER agent_multi_candidate_grant_guard ON consent_grants;
DROP FUNCTION birdtie_multi_candidate_grant_guard();
DROP TABLE agent_multi_candidate_bindings;
DROP FUNCTION birdtie_multi_candidate_binding_guard();
DROP FUNCTION birdtie_multi_candidate_preview_current(uuid);
DROP TABLE agent_multi_candidate_previews;
DROP FUNCTION birdtie_multi_candidate_preview_guard();
DROP FUNCTION birdtie_multi_candidate_sources_current(jsonb,uuid,uuid,uuid,text);
DROP FUNCTION birdtie_candidate_pipeline_single_current(uuid);
ALTER TABLE agent_effect_ledger DROP CONSTRAINT candidate_effect_native_binding;
ALTER TABLE agent_effect_ledger ADD CONSTRAINT candidate_effect_native_binding CHECK(candidate_id IS NOT NULL AND retention_grant_id IS NOT NULL AND event_id IS NOT NULL AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2') AND fence>0 AND attempt BETWEEN 1 AND 20 AND attempt<=fence);
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_handler_version_check;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_handler_version_check CHECK(handler_version IN('mom-control-v1','mom-control-v2','mom-candidate-local-v1','mom-candidate-local-v2'));
ALTER TABLE agent_consumer_inbox DROP CONSTRAINT agent_consumer_inbox_check1;
ALTER TABLE agent_consumer_inbox ADD CONSTRAINT agent_consumer_inbox_check1 CHECK((control_state='LEASED' AND reason_code='') OR(control_state='UNAVAILABLE' AND reason_code='PURPOSE_UNAVAILABLE') OR(control_state='INVALIDATED' AND reason_code='SOURCE_INVALIDATED') OR(control_state='EXPIRED' AND reason_code='EXPIRED') OR(control_state='DEAD_LETTER' AND reason_code='ATTEMPTS_EXHAUSTED') OR(control_state='CANDIDATE_STAGED' AND reason_code='CANDIDATE_STAGED' AND handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2')));
COMMIT;
