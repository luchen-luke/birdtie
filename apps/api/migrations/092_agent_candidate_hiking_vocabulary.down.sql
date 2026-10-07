BEGIN;
-- Lock before testing history: a concurrent preview/approval cannot slip past.
LOCK TABLE agent_candidate_retention_previews,agent_multi_candidate_previews,agent_memory_candidates,agent_effect_ledger,consent_grants,agent_candidate_retention_bindings,agent_multi_candidate_bindings,agent_domain_outbox,agent_consumer_inbox IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_candidate_retention_previews WHERE algorithm_version='moment-lexical-category-v2')
 OR EXISTS(SELECT 1 FROM agent_multi_candidate_previews WHERE algorithm_version='moment-lexical-category-v2')
 OR EXISTS(SELECT 1 FROM agent_memory_candidates WHERE category='hiking')
 -- Effect proof_review is transient and persistently NULL. If somebody has
 -- removed preview/binding metadata, we cannot classify its algorithm as v1;
 -- unknown is not evidence that all v2 history disappeared.
 OR EXISTS(SELECT 1 FROM agent_effect_ledger e WHERE e.candidate_id IS NOT NULL
 AND e.handler_version IN('mom-candidate-local-v1','mom-candidate-local-v2','mom-candidate-multi-v1')
 AND NOT EXISTS(SELECT 1 FROM agent_candidate_retention_bindings b JOIN agent_candidate_retention_previews p ON p.id=b.preview_id WHERE b.grant_id=e.retention_grant_id AND p.algorithm_version='moment-lexical-category-v1')
 AND NOT EXISTS(SELECT 1 FROM agent_multi_candidate_bindings b JOIN agent_multi_candidate_previews p ON p.id=b.preview_id WHERE b.grant_id=e.retention_grant_id AND p.algorithm_version='moment-lexical-category-v1')) THEN
 RAISE EXCEPTION '092 v2 preview or hiking candidate history prevents down' USING ERRCODE='55000';END IF;
END $$;
CREATE OR REPLACE FUNCTION birdtie_multi_candidate_effect_validate(e agent_effect_ledger) RETURNS void LANGUAGE plpgsql SET TimeZone='UTC' AS $$
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
DROP FUNCTION birdtie_candidate_algorithm_vocabulary(text,text);
DO $vocab$ DECLARE cn text; total integer; BEGIN
 SELECT count(*),min(conname) INTO total,cn FROM pg_constraint
 WHERE conrelid='public.agent_memory_candidates'::regclass AND contype='c'
 AND pg_get_constraintdef(oid) LIKE '%badminton%' AND pg_get_constraintdef(oid) LIKE '%culture%' AND pg_get_constraintdef(oid) LIKE '%predicate%';
 IF total<>1 THEN RAISE EXCEPTION 'candidate vocabulary constraint missing or ambiguous' USING ERRCODE='55000';END IF;
 EXECUTE format('ALTER TABLE public.agent_memory_candidates DROP CONSTRAINT %I',cn);
 EXECUTE format($constraint$ALTER TABLE public.agent_memory_candidates ADD CONSTRAINT %I CHECK (coalesce((status IN('CANDIDATE','ACTIVE') AND predicate='ACTIVITY_CATEGORY' AND category IN('badminton','basketball','football','sports','culture') AND jsonb_typeof(assessment)='object' AND jsonb_typeof(sources)='array' AND jsonb_array_length(sources) BETWEEN 1 AND 20) OR (status IN('REJECTED','SUPERSEDED','EXPIRED') AND predicate IS NULL AND category IS NULL AND assessment IS NULL AND sources='[]'::jsonb),false))$constraint$,cn);
END $vocab$;
ALTER TABLE agent_candidate_retention_previews DROP CONSTRAINT agent_candidate_retention_previews_algorithm_version_check;
ALTER TABLE agent_candidate_retention_previews ADD CONSTRAINT agent_candidate_retention_previews_algorithm_version_check CHECK(algorithm_version ='moment-lexical-category-v1');
ALTER TABLE agent_multi_candidate_previews DROP CONSTRAINT agent_multi_candidate_previews_algorithm_version_check;
ALTER TABLE agent_multi_candidate_previews ADD CONSTRAINT agent_multi_candidate_previews_algorithm_version_check CHECK(algorithm_version ='moment-lexical-category-v1');
COMMIT;
