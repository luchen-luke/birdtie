-- Atomic, owned synthetic fixtures. Never alter a seed or borrowed identity.
-- All expected SQL failures use subtransactions and exact SQLSTATE classes.
CREATE FUNCTION pg_temp.visibility_assert_rejected(statement text,label text,allowed_states text[])
RETURNS void LANGUAGE plpgsql AS $$
DECLARE rejected boolean:=false;
BEGIN
    BEGIN EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE<>ALL(allowed_states) THEN RAISE; END IF;
        rejected:=true;
    END;
    IF NOT rejected THEN RAISE EXCEPTION 'field visibility constraint accepted: %',label; END IF;
END $$;

CREATE FUNCTION pg_temp.visibility_assert_allowed(target uuid,viewer uuid,field_key text,want boolean,label text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE actual boolean;
BEGIN
    actual:=birdtie_agent_profile_field_allowed(target,viewer,field_key);
    IF actual IS DISTINCT FROM want THEN RAISE EXCEPTION 'field helper mismatch: % expected % got %',label,want,actual; END IF;
END $$;

CREATE FUNCTION pg_temp.visibility_replace(target_agent uuid,target_owner uuid,new_rules jsonb)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE next_version bigint;
BEGIN
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=target_agent RETURNING profile_version INTO next_version;
    INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version)
    VALUES(target_agent,target_owner,new_rules,next_version)
    ON CONFLICT(agent_id) DO UPDATE SET rules=EXCLUDED.rules,written_profile_version=EXCLUDED.written_profile_version;
END $$;

DO $$
DECLARE
    owner_a uuid:=gen_random_uuid(); friend_b uuid:=gen_random_uuid(); outsider_c uuid:=gen_random_uuid(); spare_owner uuid:=gen_random_uuid();
    organization_owner uuid:=gen_random_uuid(); business_owner uuid:=gen_random_uuid();
    agent_a uuid:=gen_random_uuid(); agent_b uuid:=gen_random_uuid(); agent_c uuid:=gen_random_uuid();
    organization_agent uuid:=gen_random_uuid(); business_agent uuid:=gen_random_uuid();
    cascade_owner uuid:=gen_random_uuid(); cascade_agent uuid:=gen_random_uuid();
    city_key text:='visibility-owned-'||gen_random_uuid()::text;
    community_ids uuid[]:=ARRAY[]::uuid[]; owned_community_id uuid;
    friend_request uuid:=gen_random_uuid(); conversation_request uuid:=gen_random_uuid(); tie_id uuid:=gen_random_uuid();
    defaults jsonb; valid_rules jsonb; candidate jsonb; malformed jsonb; community_rule jsonb;
    private_fields jsonb:='{"personalPreferences":["合成可见性偏好"],"socialPreferences":["合成私密说明"],"availability":"合成仅Agent目标说明",
        "preferredActivityTypes":[],"travelPreferences":[],"interactionPreferences":[],"privateCityHistory":"合成历史",
        "languagePreferences":["中文"],"agentNotes":"合成可见性测试内容"}'::jsonb;
    case_item record; field_key text; audience text; mutation text;
    before_row jsonb; after_row jsonb; private_before jsonb;
    rejection_count integer:=0; helper_count integer:=0; affected integer; revision bigint;
BEGIN
    INSERT INTO accounts(id,account_type) VALUES(owner_a,'person'),(friend_b,'person'),(outsider_c,'person'),(spare_owner,'person'),
        (organization_owner,'organization'),(business_owner,'business'),(cascade_owner,'person');
    INSERT INTO agents(id,agent_type,principal_account_id,status) VALUES(agent_a,'personal',owner_a,'active'),
        (agent_b,'personal',friend_b,'active'),(agent_c,'personal',outsider_c,'active'),
        (organization_agent,'organization',organization_owner,'active'),(business_agent,'business',business_owner,'suspended'),
        (cascade_agent,'personal',cascade_owner,'active');
    INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES
        (owner_a,'合成本人资料','合成个人简介','public'),(friend_b,'合成朋友资料','','public'),(outsider_c,'合成另一人','','public');
    INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)
        VALUES(city_key,'合成验收城市','合成','GB','Europe/London','published','local synthetic','owned fixture','local verifier');
    FOR index IN 1..9 LOOP
        owned_community_id:=gen_random_uuid(); community_ids:=array_append(community_ids,owned_community_id);
        INSERT INTO communities(id,city_id,owner_account_id,name,visibility,publication_status,owner_confirmed_at,
            source_label,source_ref,maintainer_label,expires_at)
        VALUES(owned_community_id,NULL,outsider_c,'合成明确社群'||index,'hidden','published',clock_timestamp(),
            'local synthetic','owned fixture','local verifier',clock_timestamp()+interval '1 hour');
        INSERT INTO community_memberships(community_id,user_account_id,role,status)
            VALUES(owned_community_id,owner_a,'member','active'),(owned_community_id,friend_b,'member','active');
    END LOOP;
    SELECT jsonb_object_agg(key,jsonb_build_object('visibility',CASE WHEN key IN('displayName','bio') THEN 'PUBLIC' ELSE 'PRIVATE' END,
        'communityIds','[]'::jsonb)) INTO defaults FROM unnest(ARRAY['displayName','bio','personalPreferences','socialPreferences','availability',
        'preferredActivityTypes','travelPreferences','interactionPreferences','privateCityHistory','languagePreferences','agentNotes']) key;
    FOR case_item IN SELECT * FROM (VALUES
        (owner_a,NULL::uuid,'displayName',true,'ordinary default extra gate'),
        (owner_a,friend_b,'bio',true,'ordinary default extra gate for Person'),
        (owner_a,NULL::uuid,'personalPreferences',false,'private default anonymous'),
        (owner_a,friend_b,'agentNotes',false,'private default other Person'),
        (owner_a,owner_a,'availability',true,'owner control is not model permission'),
        (owner_a,owner_a,NULL::text,false,'NULL key even self'),
        (owner_a,owner_a,'avatar',false,'unknown key even self'),
        (NULL::uuid,owner_a,'displayName',false,'NULL target'),
        (owner_a,gen_random_uuid(),'displayName',false,'unknown supplied viewer'),
        (gen_random_uuid(),NULL::uuid,'displayName',false,'unknown target')
    ) c(target,viewer,key,want,label) LOOP
        PERFORM pg_temp.visibility_assert_allowed(case_item.target,case_item.viewer,case_item.key,case_item.want,case_item.label);
        helper_count:=helper_count+1;
    END LOOP;
    FOR field_key IN SELECT key FROM jsonb_each(defaults) LOOP
        PERFORM pg_temp.visibility_assert_allowed(owner_a,owner_a,field_key,true,'every real field owner privacy control'); helper_count:=helper_count+1;
        PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,field_key,field_key IN('displayName','bio'),'every real field conservative default'); helper_count:=helper_count+1;
    END LOOP;
    PERFORM pg_temp.visibility_assert_rejected(format(
        'INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,1)',
        agent_a,owner_a,defaults),'policy at initial revision without CAS',ARRAY['23514']); rejection_count:=rejection_count+1;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id IN(agent_a,agent_b,agent_c,organization_agent,business_agent,cascade_agent);
    INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(agent_a,owner_a,private_fields,2);
    SELECT to_jsonb(p) INTO private_before FROM agent_private_profiles p WHERE agent_id=agent_a;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=agent_a AND profile_version=2;
    GET DIAGNOSTICS affected=ROW_COUNT; IF affected<>1 THEN RAISE EXCEPTION 'private v2 then policy CAS v3 failed'; END IF;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=agent_a AND profile_version=2;
    GET DIAGNOSTICS affected=ROW_COUNT; IF affected<>0 THEN RAISE EXCEPTION 'stale policy CAS wrote metadata'; END IF;

    FOR case_item IN SELECT * FROM (VALUES
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',agent_a,owner_a,defaults),'stale written version',ARRAY['P0001']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,4)',agent_a,owner_a,defaults),'future written version',ARRAY['P0001']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,3)',agent_a,friend_b,defaults),'wrong current owner',ARRAY['23503']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,3)',gen_random_uuid(),owner_a,defaults),'unknown Agent',ARRAY['P0001']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,NULL,%L::jsonb,3)',agent_a,defaults),'NULL owner',ARRAY['23502']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,owner_type,rules,written_profile_version) VALUES(%L,%L,NULL,%L::jsonb,3)',agent_a,owner_a,defaults),'NULL owner type',ARRAY['23502']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,owner_type,rules,written_profile_version) VALUES(%L,%L,''COMMUNITY'',%L::jsonb,3)',agent_a,owner_a,defaults),'Community is not policy principal',ARRAY['23514']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,NULL)',agent_a,owner_a,defaults),'NULL written version',ARRAY['P0001']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version,created_at,updated_at) VALUES(%L,%L,%L::jsonb,3,''infinity'',''infinity'')',agent_a,owner_a,defaults),'finite policy times',ARRAY['23514']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version,created_at,updated_at) VALUES(%L,%L,%L::jsonb,3,now(),now()-interval ''1 second'')',agent_a,owner_a,defaults),'ordered policy times',ARRAY['23514']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,NULL,3)',agent_a,owner_a),'SQL NULL rules',ARRAY['P0001','23502']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,owner_type,rules,written_profile_version) VALUES(%L,%L,''ORGANIZATION'',%L::jsonb,2)',organization_agent,organization_owner,defaults),'Organization policy',ARRAY['23514']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,owner_type,rules,written_profile_version) VALUES(%L,%L,''BUSINESS'',%L::jsonb,2)',business_agent,business_owner,defaults),'Business policy',ARRAY['23514']),
        (format('INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',organization_agent,organization_owner,defaults),'Person policy on Organization binding',ARRAY['23503']),
        (format('UPDATE agents SET principal_account_id=%L WHERE id=%L',spare_owner,agent_a),'current Agent identity reparent',ARRAY['23503']),
        (format('UPDATE accounts SET account_type=''organization'' WHERE id=%L',owner_a),'current owner identity retype',ARRAY['23503']),
        (format('UPDATE agent_profiles SET profile_version=5 WHERE agent_id=%L',agent_a),'base revision jump',ARRAY['P0001'])
    ) c(statement,label,states) LOOP
        PERFORM pg_temp.visibility_assert_rejected(case_item.statement,case_item.label,case_item.states); rejection_count:=rejection_count+1;
    END LOOP;
    FOR malformed IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
        'null'::jsonb,'[]'::jsonb,'{}'::jsonb,'"text"'::jsonb,
        defaults-'agentNotes',defaults||jsonb_build_object('avatar',defaults->'bio'),
        jsonb_set(defaults,'{agentNotes}','null'::jsonb),jsonb_set(defaults,'{agentNotes}','[]'::jsonb),
        jsonb_set(defaults,'{agentNotes}','"PRIVATE"'::jsonb),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('communityIds','[]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','PRIVATE')),
        jsonb_set(defaults,'{agentNotes,visibility}','null'::jsonb),
        jsonb_set(defaults,'{agentNotes,visibility}','true'::jsonb),
        jsonb_set(defaults,'{agentNotes,visibility}','"CLOSE"'::jsonb),
        jsonb_set(defaults,'{agentNotes,visibility}','"public"'::jsonb),
        jsonb_set(defaults,'{agentNotes}',(defaults->'agentNotes')||'{"confirmed":true}'::jsonb),
        jsonb_set(defaults,'{agentNotes,communityIds}','null'::jsonb),
        jsonb_set(defaults,'{agentNotes,communityIds}','{}'::jsonb),
        jsonb_set(defaults,'{agentNotes,communityIds}','"group"'::jsonb),
        jsonb_set(defaults,'{agentNotes,communityIds}',jsonb_build_array(community_ids[1])),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds','[]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds','[null]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds','[12]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds','["no-id"]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds','["00000000-0000-0000-0000-000000000000"]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds','["AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA"]'::jsonb)),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds',jsonb_build_array(gen_random_uuid()))),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds',jsonb_build_array(community_ids[1],community_ids[1]))),
        jsonb_set(defaults,'{agentNotes}',jsonb_build_object('visibility','COMMUNITY','communityIds',to_jsonb(community_ids)))
    )) LOOP
        PERFORM pg_temp.visibility_assert_rejected(format(
            'INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,3)',
            agent_a,owner_a,malformed),'rule unknown/null/type/audience/duplicate/target/cardinality',ARRAY['P0001','23514','22023']); rejection_count:=rejection_count+1;
    END LOOP;
    community_rule:=jsonb_build_object('visibility','COMMUNITY','communityIds',jsonb_build_array(community_ids[1]));
    candidate:=jsonb_set(defaults,'{agentNotes}',community_rule);
    FOR mutation IN SELECT unnest(ARRAY[
        'UPDATE communities SET lifecycle_status=''archived'' WHERE id=',
        'UPDATE communities SET publication_status=''draft'' WHERE id=',
        'UPDATE communities SET publication_status=''hidden'' WHERE id=',
        'UPDATE communities SET expires_at=clock_timestamp()-interval ''1 second'' WHERE id='
    ]) LOOP
        EXECUTE mutation||quote_literal(community_ids[1]);
        PERFORM pg_temp.visibility_assert_rejected(format(
            'INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,3)',
            agent_a,owner_a,candidate),'Community must be current/published/unexpired',ARRAY['P0001']); rejection_count:=rejection_count+1;
        UPDATE communities SET lifecycle_status='active',publication_status='published',expires_at=clock_timestamp()+interval '1 hour' WHERE id=community_ids[1];
    END LOOP;
    FOREACH audience IN ARRAY ARRAY['pending','invited','left','rejected'] LOOP
        UPDATE community_memberships SET status=audience WHERE community_id=community_ids[1] AND user_account_id=owner_a;
        PERFORM pg_temp.visibility_assert_rejected(format(
            'INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(%L,%L,%L::jsonb,3)',
            agent_a,owner_a,candidate),'owner membership must be active',ARRAY['P0001']); rejection_count:=rejection_count+1;
    END LOOP;
    UPDATE community_memberships SET status='active' WHERE community_id=community_ids[1] AND user_account_id=owner_a;
    valid_rules:=jsonb_set(defaults,'{bio,visibility}','"CONNECTIONS"'::jsonb);
    valid_rules:=jsonb_set(valid_rules,'{personalPreferences}',community_rule);
    valid_rules:=jsonb_set(valid_rules,'{availability,visibility}','"AGENT_ONLY"'::jsonb);
    valid_rules:=jsonb_set(valid_rules,'{agentNotes,visibility}','"PUBLIC"'::jsonb);
    INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version) VALUES(agent_a,owner_a,valid_rules,3);
    SELECT to_jsonb(v) INTO before_row FROM agent_profile_field_visibility v WHERE agent_id=agent_a;
    FOR case_item IN SELECT * FROM (VALUES
        (format('DELETE FROM agent_profile_field_visibility WHERE agent_id=%L',agent_a),'clear without advance'),
        (format('UPDATE agent_profile_field_visibility SET rules=%L::jsonb WHERE agent_id=%L',defaults,agent_a),'repeat written revision'),
        (format('UPDATE agent_profile_field_visibility SET written_profile_version=4 WHERE agent_id=%L',agent_a),'new written revision without CAS')
    ) c(statement,label) LOOP
        PERFORM pg_temp.visibility_assert_rejected(case_item.statement,case_item.label,ARRAY['P0001']); rejection_count:=rejection_count+1;
    END LOOP;
    SELECT to_jsonb(v) INTO after_row FROM agent_profile_field_visibility v WHERE agent_id=agent_a;
    IF before_row IS DISTINCT FROM after_row THEN RAISE EXCEPTION 'rejected policy update changed full row'; END IF;

    FOR case_item IN SELECT * FROM (VALUES
        (owner_a,NULL::uuid,'displayName',true,'PUBLIC anonymous extra gate'),
        (owner_a,friend_b,'agentNotes',true,'explicit private field PUBLIC extra gate'),
        (owner_a,friend_b,'socialPreferences',false,'PRIVATE other Person'),
        (owner_a,NULL::uuid,'availability',false,'AGENT_ONLY anonymous'),
        (owner_a,friend_b,'availability',false,'AGENT_ONLY other Person'),
        (owner_a,owner_a,'availability',true,'AGENT_ONLY owner controls'),
        (owner_a,friend_b,'personalPreferences',true,'hidden explicit Community real active members'),
        (owner_a,spare_owner,'personalPreferences',false,'unrelated Community outsider'),
        (owner_a,organization_owner,'personalPreferences',false,'Organization does not inherit member field'),
        (owner_a,business_owner,'bio',false,'Business not friend'),
        (owner_a,friend_b,'bio',false,'no Tie even same Community')
    ) c(target,viewer,key,want,label) LOOP
        PERFORM pg_temp.visibility_assert_allowed(case_item.target,case_item.viewer,case_item.key,case_item.want,case_item.label); helper_count:=helper_count+1;
    END LOOP;
    INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
        VALUES(friend_request,owner_a,friend_b,NULL,'合成明确好友','friend','pending',clock_timestamp()+interval '1 hour'),
              (conversation_request,owner_a,outsider_c,city_key,'合成聊天不当好友','conversation','accepted',clock_timestamp()+interval '1 hour');
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'pending friend not Tie'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,outsider_c,'bio',false,'accepted conversation not Tie'); helper_count:=helper_count+1;
    UPDATE connection_requests SET state='accepted' WHERE id=friend_request;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'accepted request alone not durable Tie'); helper_count:=helper_count+1;
    INSERT INTO person_ties(id,person_a_account_id,person_b_account_id,request_id)
        VALUES(tie_id,LEAST(owner_a,friend_b),GREATEST(owner_a,friend_b),friend_request);
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',true,'strong current friend Tie'); helper_count:=helper_count+1;
    UPDATE connection_requests SET state='withdrawn' WHERE id=friend_request;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'request withdrawn with stale active Tie'); helper_count:=helper_count+1;
    UPDATE connection_requests SET state='accepted',scope='conversation',city_id=city_key WHERE id=friend_request;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'conversation scope with stale Tie'); helper_count:=helper_count+1;
    UPDATE connection_requests SET scope='friend',city_id=NULL,recipient_account_id=spare_owner WHERE id=friend_request;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'wrong accepted pair with stale Tie'); helper_count:=helper_count+1;
    UPDATE connection_requests SET recipient_account_id=friend_b WHERE id=friend_request;
    UPDATE person_ties SET status='removed' WHERE id=tie_id;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'removed Tie'); helper_count:=helper_count+1;
    INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES(friend_b,owner_a);
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'agentNotes',false,'reverse Block defeats PUBLIC'); helper_count:=helper_count+1;
    DELETE FROM account_blocks WHERE blocker_account_id=friend_b AND blocked_account_id=owner_a;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'Unblock does not restore removed Tie'); helper_count:=helper_count+1;
    UPDATE person_ties SET status='active' WHERE id=tie_id;
    INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES(owner_a,friend_b);
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'bio',false,'forward Block defeats friend'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'personalPreferences',false,'Block defeats Community'); helper_count:=helper_count+1;
    DELETE FROM account_blocks WHERE blocker_account_id=owner_a AND blocked_account_id=friend_b;
    FOREACH audience IN ARRAY ARRAY['pending','invited','left','rejected'] LOOP
        UPDATE community_memberships SET status=audience WHERE community_id=community_ids[1] AND user_account_id=friend_b;
        PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'personalPreferences',false,'nonactive viewer Community status'); helper_count:=helper_count+1;
    END LOOP;
    UPDATE community_memberships SET status='active' WHERE community_id=community_ids[1] AND user_account_id=friend_b;
    UPDATE community_memberships SET status='left' WHERE community_id=community_ids[1] AND user_account_id=owner_a;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'personalPreferences',false,'owner left explicit Community'); helper_count:=helper_count+1;
    UPDATE community_memberships SET status='active' WHERE community_id=community_ids[1] AND user_account_id=owner_a;
    FOR mutation IN SELECT unnest(ARRAY[
        'UPDATE communities SET lifecycle_status=''archived'' WHERE id=',
        'UPDATE communities SET publication_status=''hidden'' WHERE id=',
        'UPDATE communities SET expires_at=clock_timestamp()-interval ''1 second'' WHERE id='
    ]) LOOP
        EXECUTE mutation||quote_literal(community_ids[1]);
        PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'personalPreferences',false,'Community withdrawn/expired after configuration'); helper_count:=helper_count+1;
        UPDATE communities SET lifecycle_status='active',publication_status='published',expires_at=clock_timestamp()+interval '1 hour' WHERE id=community_ids[1];
    END LOOP;
    UPDATE accounts SET status='suspended' WHERE id=friend_b;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'agentNotes',false,'inactive viewer even PUBLIC'); helper_count:=helper_count+1;
    UPDATE accounts SET status='active' WHERE id=friend_b;
    UPDATE accounts SET status='suspended' WHERE id=owner_a;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,NULL,'agentNotes',false,'inactive target anonymous'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,owner_a,'availability',false,'inactive target self control'); helper_count:=helper_count+1;
    UPDATE accounts SET status='active' WHERE id=owner_a;
    UPDATE agents SET status='suspended' WHERE id=agent_a;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,NULL,'displayName',false,'inactive Personal Agent no default public name'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,owner_a,'agentNotes',false,'inactive Personal Agent self'); helper_count:=helper_count+1;
    UPDATE agents SET status='active' WHERE id=agent_a;

    candidate:=jsonb_set(valid_rules,'{personalPreferences}',jsonb_build_object('visibility','COMMUNITY','communityIds',to_jsonb(community_ids[1:8])));
    PERFORM pg_temp.visibility_replace(agent_a,owner_a,candidate);
    IF (SELECT jsonb_array_length(rules->'personalPreferences'->'communityIds') FROM agent_profile_field_visibility WHERE agent_id=agent_a)<>8 THEN
        RAISE EXCEPTION 'legal maximum eight explicit Community targets failed';
    END IF;
    SELECT profile_version INTO revision FROM agent_profiles WHERE agent_id=agent_a;
    FOR case_item IN SELECT * FROM (VALUES
        (format('UPDATE agent_profile_field_visibility SET owner_id=%L,written_profile_version=%s WHERE agent_id=%L',friend_b,revision,agent_a),'immutable owner'),
        (format('UPDATE agent_profile_field_visibility SET owner_type=''ORGANIZATION'',written_profile_version=%s WHERE agent_id=%L',revision,agent_a),'immutable owner type'),
        (format('UPDATE agent_profile_field_visibility SET agent_id=%L,written_profile_version=2 WHERE agent_id=%L',agent_b,agent_a),'immutable Agent'),
        (format('UPDATE agent_profile_field_visibility SET created_at=created_at+interval ''1 second'',written_profile_version=%s WHERE agent_id=%L',revision,agent_a),'immutable creation')
    ) c(statement,label) LOOP
        PERFORM pg_temp.visibility_assert_rejected(case_item.statement,case_item.label,ARRAY['P0001']); rejection_count:=rejection_count+1;
    END LOOP;
    SELECT to_jsonb(p) INTO after_row FROM agent_private_profiles p WHERE agent_id=agent_a;
    IF private_before IS DISTINCT FROM after_row THEN RAISE EXCEPTION 'field policy changed saved private content/source revision'; END IF;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=agent_a;
    DELETE FROM agent_profile_field_visibility WHERE agent_id=agent_a;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,NULL,'agentNotes',false,'cleared PUBLIC private field returns PRIVATE default'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,friend_b,'personalPreferences',false,'cleared Community does not become public'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,owner_a,'agentNotes',true,'cleared policy still owner privacy control'); helper_count:=helper_count+1;
    SELECT to_jsonb(p) INTO after_row FROM agent_private_profiles p WHERE agent_id=agent_a;
    IF private_before IS DISTINCT FROM after_row THEN RAISE EXCEPTION 'policy clear erased or changed Private'; END IF;
    PERFORM pg_temp.visibility_replace(cascade_agent,cascade_owner,defaults);
    DELETE FROM agent_profiles WHERE agent_id=cascade_agent;
    IF EXISTS(SELECT 1 FROM agent_profile_field_visibility WHERE agent_id=cascade_agent) THEN RAISE EXCEPTION 'metadata cascade retained policy'; END IF;
    PERFORM pg_temp.visibility_assert_allowed(cascade_owner,NULL,'displayName',false,'deleted metadata cannot revive default public'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_replace(agent_a,owner_a,valid_rules);
    DELETE FROM agents WHERE id=agent_a;
    IF EXISTS(SELECT 1 FROM agent_profile_field_visibility WHERE agent_id=agent_a) OR EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id=agent_a) THEN
        RAISE EXCEPTION 'Agent deletion retained policy/private source';
    END IF;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,NULL,'displayName',false,'deleted Personal Agent cannot revive public ordinary name'); helper_count:=helper_count+1;
    PERFORM pg_temp.visibility_assert_allowed(owner_a,owner_a,'agentNotes',false,'deleted Personal Agent cannot retain self private authority'); helper_count:=helper_count+1;

    DELETE FROM person_ties WHERE id=tie_id;
    DELETE FROM connection_requests WHERE id IN(friend_request,conversation_request);
    DELETE FROM communities WHERE id=ANY(community_ids);
    DELETE FROM cities WHERE id=city_key;
    DELETE FROM agents WHERE id IN(agent_a,agent_b,agent_c,organization_agent,business_agent,cascade_agent);
    DELETE FROM accounts WHERE id IN(owner_a,friend_b,outsider_c,spare_owner,organization_owner,business_owner,cascade_owner);
    IF EXISTS(SELECT 1 FROM accounts WHERE id IN(owner_a,friend_b,outsider_c,spare_owner,organization_owner,business_owner,cascade_owner)) OR
       EXISTS(SELECT 1 FROM communities WHERE id=ANY(community_ids)) OR
       EXISTS(SELECT 1 FROM agent_profile_field_visibility WHERE agent_id IN(agent_a,cascade_agent)) THEN RAISE EXCEPTION 'owned field fixture retained data'; END IF;
    RAISE NOTICE '[PASS] % strict policy SQL rejection assertions; % actual helper permission assertions; private v2/policy v3, CAS/clear/parent deletion',rejection_count,helper_count;
END $$;
