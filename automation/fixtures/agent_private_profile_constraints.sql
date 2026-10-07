-- Owned synthetic DDL assertions. No seed or pre-existing row is modified.
-- All failed attempts use PL/pgSQL subtransactions and all owned identities
-- are removed before this atomic fixture returns successfully.
CREATE FUNCTION pg_temp.private_profile_assert_rejected(statement text, label text, allowed_states text[])
RETURNS void LANGUAGE plpgsql AS $$
DECLARE failed boolean := false;
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE<>ALL(allowed_states) THEN RAISE; END IF;
        failed := true;
    END;
    IF NOT failed THEN RAISE EXCEPTION 'private constraint accepted: %',label; END IF;
END $$;

DO $$
DECLARE
    person_a uuid := gen_random_uuid(); person_b uuid := gen_random_uuid();
    org_owner uuid := gen_random_uuid(); business_owner uuid := gen_random_uuid();
    personal_agent uuid := gen_random_uuid(); second_agent uuid := gen_random_uuid();
    organization_agent uuid := gen_random_uuid(); business_agent uuid := gen_random_uuid();
    metadata_cascade_agent uuid := gen_random_uuid(); account_cascade_agent uuid := gen_random_uuid();
    metadata_cascade_owner uuid := gen_random_uuid(); account_cascade_owner uuid := gen_random_uuid();
    valid_fields jsonb := '{"personalPreferences":[],"socialPreferences":[],"availability":"",
        "preferredActivityTypes":[],"travelPreferences":[],"interactionPreferences":[],
        "privateCityHistory":"","languagePreferences":[],"agentNotes":"合成迁移私密字段"}'::jsonb;
    malformed jsonb; candidate jsonb; large_list jsonb;
    before_row jsonb; after_row jsonb;
    affected integer; assertion_count integer := 0;
BEGIN
    INSERT INTO accounts(id,account_type) VALUES(person_a,'person'),(person_b,'person'),
        (org_owner,'organization'),(business_owner,'business'),
        (metadata_cascade_owner,'person'),(account_cascade_owner,'person');
    INSERT INTO agents(id,agent_type,principal_account_id,status) VALUES
        (personal_agent,'personal',person_a,'active'),(second_agent,'personal',person_b,'active'),
        (organization_agent,'organization',org_owner,'active'),
        (business_agent,'business',business_owner,'suspended'),
        (metadata_cascade_agent,'personal',metadata_cascade_owner,'active'),
        (account_cascade_agent,'personal',account_cascade_owner,'active');

    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,1)',
        personal_agent,person_a,valid_fields),'initial content without authoritative CAS',ARRAY['23514']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_profiles SET profile_version=3 WHERE agent_id=%L',personal_agent),
        'base revision must advance exactly once',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    UPDATE agent_profiles SET profile_version=profile_version+1
        WHERE agent_id IN (personal_agent,second_agent,organization_agent,business_agent,
            metadata_cascade_agent,account_cascade_agent) AND profile_version=1;
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>6 THEN RAISE EXCEPTION 'own metadata initial CAS failed'; END IF;

    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,3)',
        personal_agent,person_a,valid_fields),'future written revision',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',
        personal_agent,person_b,valid_fields),'different Person owner binding',ARRAY['23503']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,owner_type,fields,written_profile_version) VALUES(%L,%L,''ORGANIZATION'',%L::jsonb,2)',
        organization_agent,org_owner,valid_fields),'Organization private profile',ARRAY['23514']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,owner_type,fields,written_profile_version) VALUES(%L,%L,''BUSINESS'',%L::jsonb,2)',
        business_agent,business_owner,valid_fields),'Business private profile',ARRAY['23514']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',
        gen_random_uuid(),person_a,valid_fields),'unknown Agent binding',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,NULL,%L::jsonb,2)',
        personal_agent,valid_fields),'NULL owner',ARRAY['23502']);
    assertion_count:=assertion_count+1;

    FOR malformed IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
        'null'::jsonb,'[]'::jsonb,'{}'::jsonb,
        valid_fields || '{"inferenceConsent":true}'::jsonb,
        valid_fields - 'agentNotes',
        jsonb_set(valid_fields,'{personalPreferences}','null'::jsonb),
        jsonb_set(valid_fields,'{personalPreferences}','"text"'::jsonb),
        jsonb_set(valid_fields,'{personalPreferences}','[null]'::jsonb),
        jsonb_set(valid_fields,'{personalPreferences}','[42]'::jsonb),
        jsonb_set(valid_fields,'{personalPreferences}','[""]'::jsonb),
        jsonb_set(valid_fields,'{personalPreferences}',jsonb_build_array(repeat('x',161))),
        jsonb_set(valid_fields,'{availability}','[]'::jsonb),
        jsonb_set(valid_fields,'{privateCityHistory}','null'::jsonb),
        jsonb_set(valid_fields,'{agentNotes}',to_jsonb(repeat('x',2001)))
    )) LOOP
        PERFORM pg_temp.private_profile_assert_rejected(format(
            'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',
            personal_agent,person_a,malformed),'unknown/null/wrong type/field bounds',ARRAY['P0001','23514','22023']);
        assertion_count:=assertion_count+1;
    END LOOP;
    SELECT jsonb_agg('item-'||n) INTO large_list FROM generate_series(1,21) n;
    candidate:=jsonb_set(valid_fields,'{languagePreferences}',large_list);
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',
        personal_agent,person_a,candidate),'list cardinality',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,NULL,2)',
        personal_agent,person_a),'SQL NULL fields',ARRAY['23502','P0001']);
    assertion_count:=assertion_count+1;
    SELECT jsonb_agg(repeat('字',160)) INTO large_list FROM generate_series(1,20);
    candidate:=jsonb_set(jsonb_set(valid_fields,'{personalPreferences}',large_list),'{socialPreferences}',large_list);
    IF octet_length(candidate::text)<=16384 THEN RAISE EXCEPTION 'byte-bound fixture is not actually oversized'; END IF;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version) VALUES(%L,%L,%L::jsonb,2)',
        personal_agent,person_a,candidate),'raw SQL byte bound',ARRAY['23514']);
    assertion_count:=assertion_count+1;

    INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version)
        VALUES(personal_agent,person_a,valid_fields,2),
              (metadata_cascade_agent,metadata_cascade_owner,valid_fields,2),
              (account_cascade_agent,account_cascade_owner,valid_fields,2);
    SELECT to_jsonb(x) INTO before_row FROM agent_private_profiles x WHERE agent_id=personal_agent;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET fields=%L::jsonb WHERE agent_id=%L',valid_fields,personal_agent),
        'unchanged written revision',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET written_profile_version=3 WHERE agent_id=%L',personal_agent),
        'written revision without base CAS',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'DELETE FROM agent_private_profiles WHERE agent_id=%L',personal_agent),
        'clear without revision advance',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    SELECT to_jsonb(x) INTO after_row FROM agent_private_profiles x WHERE agent_id=personal_agent;
    IF before_row IS DISTINCT FROM after_row THEN RAISE EXCEPTION 'rejected private updates changed content'; END IF;

    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=personal_agent AND profile_version=2;
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>1 THEN RAISE EXCEPTION 'own content revision CAS failed'; END IF;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=personal_agent AND profile_version=2;
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>0 THEN RAISE EXCEPTION 'stale own metadata CAS wrote again'; END IF;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET fields=%L::jsonb,written_profile_version=2 WHERE agent_id=%L',valid_fields,personal_agent),
        'stale content after base advance',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET owner_id=%L,written_profile_version=3 WHERE agent_id=%L',person_b,personal_agent),
        'immutable owner',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET owner_type=''ORGANIZATION'',written_profile_version=3 WHERE agent_id=%L',personal_agent),
        'immutable owner type',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET agent_id=%L,written_profile_version=2 WHERE agent_id=%L',second_agent,personal_agent),
        'immutable Agent binding',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    PERFORM pg_temp.private_profile_assert_rejected(format(
        'UPDATE agent_private_profiles SET created_at=created_at+interval ''1 second'',written_profile_version=3 WHERE agent_id=%L',personal_agent),
        'immutable creation time',ARRAY['P0001']);
    assertion_count:=assertion_count+1;
    UPDATE agent_private_profiles SET fields=jsonb_set(valid_fields,'{agentNotes}','"合成更新后的直接声明"'::jsonb),
        written_profile_version=3 WHERE agent_id=personal_agent;
    IF NOT EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id=personal_agent AND written_profile_version=3
        AND updated_at>=created_at AND fields->>'agentNotes'='合成更新后的直接声明') THEN
        RAISE EXCEPTION 'valid private same-transaction revision update failed';
    END IF;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=personal_agent AND profile_version=3;
    DELETE FROM agent_private_profiles WHERE agent_id=personal_agent;
    IF EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id=personal_agent) OR
        (SELECT profile_version FROM agent_profiles WHERE agent_id=personal_agent)<>4 THEN
        RAISE EXCEPTION 'CAS-protected clear did not retain metadata revision';
    END IF;

    DELETE FROM agent_profiles WHERE agent_id=metadata_cascade_agent;
    IF EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id=metadata_cascade_agent) THEN
        RAISE EXCEPTION 'parent metadata cascade retained private contents';
    END IF;
    DELETE FROM accounts WHERE id=account_cascade_owner;
    IF EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=account_cascade_agent) OR
        EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id=account_cascade_agent) THEN
        RAISE EXCEPTION 'parent account cascade retained private identity/content';
    END IF;
    DELETE FROM agents WHERE id IN (personal_agent,second_agent,organization_agent,business_agent,
        metadata_cascade_agent,account_cascade_agent);
    DELETE FROM accounts WHERE id IN (person_a,person_b,org_owner,business_owner,
        metadata_cascade_owner,account_cascade_owner);
    IF EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id IN (personal_agent,metadata_cascade_agent,account_cascade_agent)) OR
        EXISTS(SELECT 1 FROM accounts WHERE id IN (person_a,person_b,org_owner,business_owner,
            metadata_cascade_owner,account_cascade_owner)) THEN RAISE EXCEPTION 'owned SQL fixture left data'; END IF;
    RAISE NOTICE '[PASS] % strict rejection assertions plus valid CAS/write/clear and parent cascades',assertion_count;
END $$;
