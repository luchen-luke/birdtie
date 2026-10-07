-- Synthetic invariants; all inserted rows are rolled back.
BEGIN;
DO $$
DECLARE v_person uuid; v_org_account uuid; v_city_context uuid;
        v_online uuid; v_country uuid; v_institution uuid; v_community uuid;
        v_other_city text := 'synthetic-cross-city-ctx-033';
        v_other_context uuid; v_first_task uuid; v_second_task uuid;
        v_online_task uuid;
BEGIN
    SELECT id INTO v_person FROM accounts WHERE account_type='person' AND status='active'
        ORDER BY id LIMIT 1;
    SELECT id INTO v_org_account FROM accounts WHERE account_type='organization'
        ORDER BY id LIMIT 1;
    IF v_person IS NULL OR v_org_account IS NULL THEN
        RAISE EXCEPTION 'context graph fixture requires seeded Person and Organization';
    END IF;
    SELECT id INTO v_city_context FROM contexts WHERE context_type='CITY'
        AND city_id='aberdeen-gb';
    IF v_city_context IS NULL THEN RAISE EXCEPTION 'Aberdeen city context not backfilled'; END IF;
    INSERT INTO contexts(context_type,online_key) VALUES('ONLINE','synthetic-global-033')
        RETURNING id INTO v_online;
    INSERT INTO contexts(context_type,country_code) VALUES('COUNTRY','GB')
        RETURNING id INTO v_country;
    INSERT INTO contexts(context_type,institution_key) VALUES('INSTITUTION','synthetic-campus-033')
        RETURNING id INTO v_institution;
    INSERT INTO contexts(context_type,community_id)
        SELECT 'COMMUNITY',id FROM communities ORDER BY id LIMIT 1
        RETURNING id INTO v_community;
    IF v_online IS NULL OR v_country IS NULL OR v_institution IS NULL OR v_community IS NULL THEN
        RAISE EXCEPTION 'non-city contexts could not be created';
    END IF;
    BEGIN
        INSERT INTO contexts(context_type) VALUES('ONLINE');
        RAISE EXCEPTION 'online context accepted missing key';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    INSERT INTO person_contexts(person_account_id,context_id,relation)
        VALUES(v_person,v_online,'interest');
    IF NOT EXISTS (SELECT 1 FROM person_contexts WHERE person_account_id=v_person
        AND context_id=v_online AND visibility='private') THEN
        RAISE EXCEPTION 'Person context did not default private';
    END IF;
    BEGIN
        INSERT INTO person_contexts(person_account_id,context_id,relation)
            VALUES(v_org_account,v_online,'interest');
        RAISE EXCEPTION 'organization account accepted as Person context owner';
    EXCEPTION WHEN raise_exception THEN
        IF SQLERRM='organization account accepted as Person context owner' THEN RAISE; END IF;
    END;
    INSERT INTO cities(id,name,region,country_code,time_zone,source_label,source_ref,maintainer_label)
    VALUES(v_other_city,'Synthetic cross-city','Fixture','GB','Europe/London',
        'synthetic','ctx-033','test');
    INSERT INTO city_contexts(city_id) VALUES(v_other_city);
    SELECT id INTO v_other_context FROM contexts
        WHERE context_type='CITY' AND city_id=v_other_city;
    IF v_other_context IS NULL THEN RAISE EXCEPTION 'new CityContext did not create typed node'; END IF;
    -- Old writers send city_id and city_context_id; the trigger supplies context_id.
    INSERT INTO agent_tasks(owner_account_id,principal_type,acting_user_account_id,
        city_id,city_context_id,query,status)
    VALUES(v_person,'person',v_person,'aberdeen-gb','aberdeen-gb','旧版城市任务','ACTIVE')
    RETURNING id INTO v_first_task;
    INSERT INTO agent_tasks(owner_account_id,principal_type,acting_user_account_id,
        city_id,city_context_id,query,status)
    VALUES(v_person,'person',v_person,v_other_city,v_other_city,'另一城市任务','ACTIVE')
    RETURNING id INTO v_second_task;
    INSERT INTO agent_tasks(owner_account_id,principal_type,acting_user_account_id,
        context_type,context_id,query,status)
    VALUES(v_person,'person',v_person,'ONLINE',v_online,'纯在线任务','ACTIVE')
    RETURNING id INTO v_online_task;
    IF NOT EXISTS (SELECT 1 FROM agent_tasks WHERE id=v_first_task
        AND context_type='CITY' AND context_id=v_city_context) OR
       NOT EXISTS (SELECT 1 FROM agent_tasks WHERE id=v_second_task
        AND context_type='CITY' AND context_id=v_other_context) OR
       NOT EXISTS (SELECT 1 FROM agent_tasks WHERE id=v_online_task
        AND city_id IS NULL AND city_context_id IS NULL AND context_type='ONLINE'
        AND context_id=v_online) OR
       (SELECT count(DISTINCT owner_account_id) FROM agent_tasks
        WHERE id IN(v_first_task,v_second_task,v_online_task))<>1 THEN
        RAISE EXCEPTION 'cross-city/online task principal or Context mapping failed';
    END IF;
    BEGIN
        INSERT INTO agent_tasks(owner_account_id,principal_type,acting_user_account_id,
            city_id,city_context_id,context_type,context_id,query,status)
        VALUES(v_person,'person',v_person,'aberdeen-gb','aberdeen-gb','ONLINE',v_online,
            '错误的城市归属','ACTIVE');
        RAISE EXCEPTION 'online context accepted a fake city parent';
    EXCEPTION WHEN check_violation THEN
        NULL;
    END;
END $$;
ROLLBACK;
