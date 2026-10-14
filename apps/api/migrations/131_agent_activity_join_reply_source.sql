-- JOIN-only latest native reply source; applied 130 remains immutable.
BEGIN;
ALTER FUNCTION birdtie_activity_join_target_tail(uuid,uuid) RENAME TO birdtie_activity_join_target_tail_v130;
ALTER FUNCTION birdtie_activity_join_base_current(uuid) RENAME TO birdtie_activity_join_base_current_v130;
ALTER FUNCTION birdtie_activity_join_approval_check(agent_action_approvals,agent_action_approvals,text) RENAME TO birdtie_activity_join_approval_check_v130;

CREATE FUNCTION birdtie_activity_join_reply_source_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM agent_action_approvals p JOIN agent_tasks t ON t.id=p.task_id
 JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=t.city_id
 JOIN city_contexts cc ON cc.city_id=t.city_id AND cc.status='active'
 CROSS JOIN LATERAL(SELECT p.binding_canonical::jsonb b,p.payload_canonical::jsonb v,jsonb_array_length(t.conversation)-1 i) x
 CROSS JOIN LATERAL(SELECT t.conversation->x.i m,t.conversation->(x.i-1) u) reply
 WHERE p.id=pid AND p.action_kind='ACTIVITY_JOIN' AND x.b->>'source_contract'='LATEST_ACTIVITY_REPLY_V1'
 AND t.owner_account_id=p.owner_id AND t.principal_type='person' AND t.acting_user_account_id=p.owner_id AND t.context_type='CITY' AND t.status='COMPLETED'
 AND(t.intent='FIND_ACTIVITY' OR(t.intent='REFINE_RESULTS' AND t.filters->>'targetIntent'='FIND_ACTIVITY'))
 AND t.xmin::text=x.b->>'source_generation' AND x.i BETWEEN 1 AND 9999 AND x.b->>'reply_index'=x.i::text
 AND reply.m->>'role'='assistant' AND reply.u->>'role'='user' AND reply.u->>'text'=coalesce(nullif(t.filters->>'currentQuery',''),t.query)
 AND reply.m->'resultMembership'->>'schema'='agent-reply-membership-v1'
 AND reply.m->'resultMembership'->>'taskId'=t.id::text AND reply.m->'resultMembership'->>'cityId'=t.city_id
 AND reply.m->'resultMembership'->>'kind'='activity' AND reply.m->'resultMembership'->>'turnDigest'=x.b->>'reply_turn_digest'
 AND x.v->>'replyTurnDigest'=x.b->>'reply_turn_digest' AND(x.v->>'expectedTaskUpdatedAt')::timestamptz=t.updated_at
 AND reply.m->'resultMembership'->>'resultSetId'=x.b->>'reply_result_set_id'
 AND x.b->>'reply_result_set_id'='reply:'||t.id::text||':'||left(x.b->>'reply_turn_digest',24)
 AND jsonb_typeof(reply.m->'resultMembership'->'refs')='array' AND jsonb_array_length(reply.m->'resultMembership'->'refs') BETWEEN 1 AND 30
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(reply.m->'resultMembership'->'refs') r WHERE r->>'type'='activity' AND r->>'id'=x.b->>'target_id')
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(reply.m->'resultMembership'->'refs') r WHERE r->>'type'<>'activity'))
$$;

CREATE FUNCTION birdtie_activity_join_target_tail(aid uuid,oid uuid) RETURNS text LANGUAGE sql VOLATILE AS $$
 WITH action_field_communities AS MATERIALIZED(SELECT c.id,c.expires_at FROM communities c WHERE false)
 SELECT encode(sha256(convert_to((SELECT coalesce(jsonb_agg(jsonb_build_array(kind,id,token) ORDER BY kind,id),'[]'::jsonb) FROM (
 SELECT 'source' kind,'activity'::text id,''::text token
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(aid,oid) OR a.id IN(SELECT host_account_id FROM activities WHERE id=aid) OR a.id IN(SELECT owner_account_id FROM communities WHERE id=aid) OR a.id IN(SELECT account_id FROM organizations WHERE id=aid) OR a.id IN(SELECT account_id FROM businesses WHERE id=aid)
 UNION ALL SELECT 'activity',a.id::text,a.xmin::text FROM activities a WHERE a.id=aid
 UNION ALL SELECT 'organizer',a.activity_id::text,a.xmin::text FROM activity_organizers a WHERE a.activity_id=aid
 UNION ALL SELECT 'place',p.id::text,p.xmin::text FROM places p WHERE p.id=aid OR p.id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'city',c.id::text,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM activities WHERE id=aid UNION SELECT city_id FROM places WHERE id=aid UNION SELECT city_id FROM communities WHERE id=aid)
 UNION ALL SELECT 'profile',p.account_id::text,p.xmin::text FROM user_profiles p WHERE p.account_id=aid
 UNION ALL SELECT 'profileacl',v.agent_id::text,v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id=aid
 UNION ALL SELECT 'agent',a.id::text,a.xmin::text FROM agents a WHERE a.principal_account_id=aid
 UNION ALL SELECT 'agent-profile',p.agent_id::text,p.xmin::text FROM agent_profiles p WHERE p.owner_id=aid
 UNION ALL SELECT 'org-member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'host-account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid UNION SELECT account_id FROM organizations WHERE id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=aid) UNION SELECT account_id FROM businesses WHERE id IN(SELECT business_id FROM activity_organizers WHERE activity_id=aid) UNION SELECT owner_account_id FROM communities WHERE id IN(SELECT community_id FROM activity_organizers WHERE activity_id=aid))
 UNION ALL SELECT 'host-community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT community_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'org',o.id::text,o.xmin::text FROM organizations o WHERE o.id=aid OR o.id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id=aid OR b.id IN(SELECT business_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'community',c.id::text,c.xmin::text FROM communities c WHERE c.id=aid OR c.id IN(SELECT community_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'blocks',b.blocker_account_id::text||':'||b.blocked_account_id::text,b.xmin::text FROM account_blocks b WHERE b.blocker_account_id=oid OR b.blocked_account_id=oid
 UNION ALL SELECT 'contact-intent',i.id::text,i.xmin::text FROM intents i WHERE 'activity'='person' AND i.owner_account_id=aid
 UNION ALL SELECT 'contact-city',c.id::text,c.xmin::text FROM cities c WHERE 'activity'='person' AND c.id IN(SELECT city_id FROM intents WHERE owner_account_id=aid)
 UNION ALL SELECT 'contact-conversation',cv.id::text,concat_ws(':',cv.xmin::text,r.xmin::text) FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id WHERE 'activity'='person' AND LEAST(cv.member_a_account_id,cv.member_b_account_id)=LEAST(aid::uuid,oid::uuid) AND GREATEST(cv.member_a_account_id,cv.member_b_account_id)=GREATEST(aid::uuid,oid::uuid)
 UNION ALL SELECT 'tie',t.id::text,t.xmin::text FROM person_ties t WHERE t.person_a_account_id=LEAST(aid::uuid,oid::uuid) AND t.person_b_account_id=GREATEST(aid::uuid,oid::uuid)
 UNION ALL SELECT 'request',r.id::text,r.xmin::text FROM connection_requests r WHERE r.sender_account_id=oid OR (r.sender_account_id=oid AND r.recipient_account_id=aid) OR (r.recipient_account_id=oid AND r.sender_account_id=aid)
 UNION ALL SELECT 'participation',p.id::text,p.xmin::text FROM activity_participations p WHERE p.activity_id=aid AND p.participant_account_id<>oid
 UNION ALL SELECT 'member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.user_account_id=oid OR m.community_id=aid
 UNION ALL SELECT 'field-community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN (SELECT id FROM action_field_communities)
 UNION ALL SELECT 'field-community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT id FROM action_field_communities) AND m.user_account_id IN(aid,oid)
 UNION ALL SELECT 'invitation',i.id::text,i.xmin::text FROM activity_invitations i WHERE i.activity_id=aid
 UNION ALL SELECT 'saved',s.id::text,s.xmin::text FROM saved_items s WHERE s.owner_account_id=oid AND (s.place_id=aid OR s.activity_id=aid OR s.community_id=aid)
 UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'venuecandidate',v.id::text,v.xmin::text FROM venue_candidates v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'businessvenue',v.business_id::text||':'||v.place_id::text,v.xmin::text FROM business_venue_relations v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'venue-operator',o.id::text,concat_ws(':',o.xmin::text,a.xmin::text) FROM organizations o JOIN accounts a ON a.id=o.account_id WHERE o.id IN(SELECT operator_organization_id FROM venues WHERE place_id IN(SELECT place_id FROM activities WHERE id=aid))

 UNION ALL SELECT 'host-profile',p.account_id::text,p.xmin::text FROM user_profiles p WHERE p.account_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'host-field-gate',v.agent_id::text,v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'host-agent',a.id::text,a.xmin::text FROM agents a WHERE a.principal_account_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'host-agent-profile',p.agent_id::text,p.xmin::text FROM agent_profiles p WHERE p.owner_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'host-grant',g.id::text,g.xmin::text FROM consent_grants g WHERE g.owner_account_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid) AND g.recipient_account_id=oid AND g.resource_type='profile' AND g.purpose='profile_view'
 UNION ALL SELECT 'host-community',c.id::text,c.xmin::text FROM communities c WHERE c.id::text IN(SELECT scope.value#>>'{}' FROM agent_profile_field_visibility v CROSS JOIN LATERAL jsonb_array_elements(coalesce(v.rules->'displayName'->'communityIds','[]'::jsonb)) scope(value) WHERE v.owner_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid))
 UNION ALL SELECT 'host-community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id::text IN(SELECT scope.value#>>'{}' FROM agent_profile_field_visibility v CROSS JOIN LATERAL jsonb_array_elements(coalesce(v.rules->'displayName'->'communityIds','[]'::jsonb)) scope(value) WHERE v.owner_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid)) AND(m.user_account_id=oid OR m.user_account_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid))
 UNION ALL SELECT 'source-origin',concat_ws(':',s.activity_id,s.candidate_id),s.xmin::text FROM activity_sources s WHERE s.activity_id=aid
 UNION ALL SELECT 'host-label-current',a.id::text,jsonb_build_array(birdtie_agent_profile_field_allowed(a.id,oid,'displayName'),p.visibility='public' OR a.id=oid OR EXISTS(SELECT 1 FROM consent_grants g WHERE g.owner_account_id=a.id AND g.recipient_account_id=oid AND g.resource_type='profile' AND g.resource_id=a.id::text AND g.purpose='profile_view' AND g.actions @> ARRAY['read']::text[] AND g.revoked_at IS NULL AND(g.expires_at IS NULL OR g.expires_at>clock_timestamp())))::text FROM accounts a LEFT JOIN user_profiles p ON p.account_id=a.id WHERE a.id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=aid)
 UNION ALL SELECT 'city-context',c.city_id::text,c.xmin::text FROM city_contexts c WHERE c.city_id IN(SELECT city_id FROM activities WHERE id=aid)
 UNION ALL SELECT 'context',c.id::text,c.xmin::text FROM contexts c WHERE c.context_type='CITY' AND c.city_id IN(SELECT city_id FROM activities WHERE id=aid)
 ) tokens)::text,'UTF8')),'hex') $$;
CREATE FUNCTION birdtie_activity_join_base_current(pid uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM agent_action_approvals p JOIN sessions s ON s.id=p.session_id JOIN accounts a ON a.id=p.owner_id JOIN agents ag ON ag.id=p.agent_id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN agent_tasks t ON t.id=p.task_id JOIN activities ac ON ac.id=(p.binding_canonical::jsonb->>'target_id')::uuid JOIN cities city ON city.id=ac.city_id CROSS JOIN LATERAL(SELECT p.binding_canonical::jsonb b) x
 WHERE p.id=pid AND p.action_kind='ACTIVITY_JOIN' AND s.account_id=a.id AND a.account_type='person' AND a.status='active' AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' AND t.owner_account_id=a.id AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.status='COMPLETED' AND t.xmin::text=x.b->>'source_generation'
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=a.id AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id)
 AND birdtie_activity_join_reply_source_current(pid) AND t.city_id=ac.city_id
 AND birdtie_activity_join_reader_authority(s.token_sha256,a.id,ag.id) IS NOT NULL AND birdtie_activity_join_authority(s.id,a.id,ag.id)=x.b->>'authority_version'
 AND (SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(ps)||jsonb_build_object('_xmin',ps.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings ps WHERE ps.owner_id=a.id AND ps.agent_id=ag.id AND ps.owner_type='PERSON')=x.b->>'policy_version'
 AND birdtie_activity_join_target_tail(ac.id,a.id)=x.b->>'target_tail_version' AND p.cancelled_at IS NULL AND p.expires_at>clock_timestamp()
 AND ac.visibility='public' AND ac.publication_status='published' AND ac.cancelled_at IS NULL AND ac.ends_at>clock_timestamp() AND (ac.expires_at IS NULL OR ac.expires_at>clock_timestamp()) AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(ac.id,a.id)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE ac.host_account_id IS NOT NULL AND ((bl.blocker_account_id=a.id AND bl.blocked_account_id=ac.host_account_id) OR(bl.blocked_account_id=a.id AND bl.blocker_account_id=ac.host_account_id)))) $$;
CREATE FUNCTION birdtie_activity_join_approval_check(n agent_action_approvals,o agent_action_approvals,op text) RETURNS agent_action_approvals LANGUAGE plpgsql AS $$
DECLARE b jsonb;v jsonb;
BEGIN
 b:=n.binding_canonical::jsonb;v:=n.payload_canonical::jsonb;
 -- Existing ACTIVE approvals are immutable history, never upgraded in place.
 IF NOT(b ? 'source_contract') THEN
  IF op<>'UPDATE' OR n.cancelled_at IS NULL OR(to_jsonb(n)-'cancelled_at') IS DISTINCT FROM(to_jsonb(o)-'cancelled_at') THEN RAISE EXCEPTION 'legacy JOIN approval is history only' USING ERRCODE='23514';END IF;
  RETURN birdtie_activity_join_approval_check_v130(n,o,op);
 END IF;
 IF op='UPDATE' THEN
  IF o.action_kind<>'ACTIVITY_JOIN' OR (to_jsonb(n)-ARRAY['approved_at','consumed_at','cancelled_at']) IS DISTINCT FROM (to_jsonb(o)-ARRAY['approved_at','consumed_at','cancelled_at']) OR(o.approved_at IS NOT NULL AND n.approved_at IS DISTINCT FROM o.approved_at) OR(o.consumed_at IS NOT NULL AND n.consumed_at IS DISTINCT FROM o.consumed_at) OR(o.cancelled_at IS NOT NULL AND n.cancelled_at IS DISTINCT FROM o.cancelled_at) OR(n.consumed_at IS NOT NULL AND n.approved_at IS NULL) THEN RAISE EXCEPTION 'immutable Agent JOIN approval' USING ERRCODE='23514';END IF;
 ELSE IF n.approved_at IS NOT NULL OR n.consumed_at IS NOT NULL OR n.cancelled_at IS NOT NULL THEN RAISE EXCEPTION 'JOIN preview is not approved' USING ERRCODE='23514';END IF;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(b))<>33 OR (SELECT count(*) FROM jsonb_object_keys(v))<>6
 OR EXISTS(SELECT 1 FROM jsonb_each(b) f WHERE(f.key='grant_revision' AND f.value::text<>'1') OR(f.key<>'grant_revision' AND jsonb_typeof(f.value)<>'string'))
 OR EXISTS(SELECT 1 FROM jsonb_each(v) f WHERE jsonb_typeof(f.value)<>'string')
 OR NOT(b ?& ARRAY['schema_version','approval_id','tenant_id','actor_id','subject_type','subject_id','agent_id','session_id','task_id','logical_operation_id','action_id','tool','tool_version','target_id','payload_digest','source_version','source_generation','authority_version','policy_version','consent_purpose','grant_id','grant_revision','membership_version','observed_at','expires_at','target_version','target_tail_version','participation_version','source_contract','reply_index','reply_turn_digest','reply_result_set_id','review_digest'])
 OR NOT(v ?& ARRAY['taskId','logicalOperationId','actionId','activityId','expectedTaskUpdatedAt','replyTurnDigest']) OR b->>'schema_version'<>'air.activity_join.v1' OR b->>'approval_id'<>n.id::text OR b->>'tenant_id'<>n.owner_id::text OR b->>'actor_id'<>n.owner_id::text OR b->>'subject_id'<>n.owner_id::text OR b->>'subject_type'<>'PERSON' OR b->>'agent_id'<>n.agent_id::text OR b->>'task_id'<>n.task_id::text OR b->>'session_id'<>n.session_id::text
 OR b->>'source_contract'<>'LATEST_ACTIVITY_REPLY_V1' OR b->>'reply_index'!~'^[1-9][0-9]{0,3}$'
 OR b->>'reply_turn_digest'!~'^[0-9a-f]{64}$' OR b->>'review_digest'!~'^[0-9a-f]{64}$'
 OR b->>'reply_result_set_id'<>'reply:'||n.task_id::text||':'||left(b->>'reply_turn_digest',24)
 OR v->>'replyTurnDigest'<>b->>'reply_turn_digest' OR NOT isfinite((v->>'expectedTaskUpdatedAt')::timestamptz)
 OR b->>'tool'<>'activity.join' OR b->>'tool_version'<>'activity.join.v1' OR b->>'consent_purpose'<>'OWN_ACTIVITY_JOIN_ACTION' OR b->>'grant_id'<>n.grant_id::text OR b->>'grant_revision'<>'1' OR b->>'membership_version'<>'NONE_PERSON_ONLY'
 OR b->>'task_id'<>v->>'taskId' OR b->>'logical_operation_id'<>v->>'logicalOperationId' OR b->>'action_id'<>v->>'actionId' OR b->>'target_id'<>v->>'activityId'
 OR (b->>'observed_at')::timestamptz<>n.observed_at OR (b->>'expires_at')::timestamptz<>n.expires_at
 OR b->>'payload_digest'<>encode(sha256(convert_to('birdtie.activity-join-payload.v1:'||n.payload_canonical,'UTF8')),'hex') OR n.binding_digest<>encode(sha256(convert_to('birdtie.activity-join-approval.v1:'||n.binding_canonical,'UTF8')),'hex')
 OR EXISTS(SELECT 1 FROM unnest(ARRAY['source_version','authority_version','policy_version','target_version','target_tail_version','participation_version','reply_turn_digest','review_digest']) k WHERE (b->>k)!~'^[0-9a-f]{64}$')
 THEN RAISE EXCEPTION 'invalid closed Agent JOIN binding' USING ERRCODE='23514';END IF;RETURN n;
END $$;
COMMIT;
