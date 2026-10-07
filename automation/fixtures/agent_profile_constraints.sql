-- Owned synthetic fixtures; every successful assertion cleans its records.
DO $$
DECLARE
    person_a uuid := gen_random_uuid(); person_b uuid := gen_random_uuid();
    org_owner uuid := gen_random_uuid(); business_owner uuid := gen_random_uuid();
    personal_agent uuid := gen_random_uuid(); organization_agent uuid := gen_random_uuid();
    business_agent uuid := gen_random_uuid(); system_agent uuid := gen_random_uuid();
    rejected boolean; affected integer;
BEGIN
    INSERT INTO accounts(id,account_type) VALUES(person_a,'person'),(person_b,'person'),
        (org_owner,'organization'),(business_owner,'business');
    INSERT INTO agents(id,agent_type,principal_account_id,status) VALUES
        (personal_agent,'personal',person_a,'active'),
        (organization_agent,'organization',org_owner,'active'),
        (business_agent,'business',business_owner,'suspended'),
        (system_agent,'system',NULL,'active');
    IF (SELECT count(*) FROM agent_profiles WHERE agent_id IN (personal_agent,organization_agent,business_agent))<>3 OR
       EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=system_agent) THEN
        RAISE EXCEPTION 'shared metadata bootstrap or System exclusion failed';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=business_agent AND owner_type='BUSINESS' AND owner_id=business_owner) THEN
        RAISE EXCEPTION 'reserved Business typed binding failed';
    END IF;
    rejected := false;
    BEGIN UPDATE agents SET status='active' WHERE id=business_agent;
    EXCEPTION WHEN check_violation THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'Business Agent was enabled'; END IF;
    rejected := false;
    BEGIN INSERT INTO agents(agent_type,principal_account_id) VALUES('community',person_b);
    EXCEPTION WHEN check_violation THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'Community received Agent'; END IF;

    rejected := false;
    BEGIN UPDATE agent_profiles SET owner_id=person_b,profile_version=2 WHERE agent_id=personal_agent;
    EXCEPTION WHEN raise_exception THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'profile owner was rebound'; END IF;
    rejected := false;
    BEGIN UPDATE agents SET principal_account_id=person_b WHERE id=personal_agent;
    EXCEPTION WHEN foreign_key_violation THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'Agent principal broke profile binding'; END IF;
    rejected := false;
    BEGIN UPDATE accounts SET account_type='organization' WHERE id=person_a;
    EXCEPTION WHEN foreign_key_violation THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'account type broke profile binding'; END IF;

    DELETE FROM agent_profiles WHERE agent_id=personal_agent;
    rejected := false;
    BEGIN INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES(personal_agent,'PERSON',person_b);
    EXCEPTION WHEN foreign_key_violation THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'individual valid IDs permitted mismatched binding'; END IF;
    rejected := false;
    BEGIN INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES(personal_agent,'ORGANIZATION',person_a);
    EXCEPTION WHEN foreign_key_violation THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'wrong owner type accepted'; END IF;
    rejected := false;
    BEGIN INSERT INTO agent_profiles(agent_id,owner_type,owner_id,profile_version) VALUES(personal_agent,'PERSON',person_a,2);
    EXCEPTION WHEN raise_exception THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'noninitial inserted revision accepted'; END IF;
    INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES(personal_agent,'PERSON',person_a);

    rejected := false;
    BEGIN UPDATE agent_profiles SET profile_version=3 WHERE agent_id=personal_agent;
    EXCEPTION WHEN raise_exception THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'jumped revision accepted'; END IF;
    rejected := false;
    BEGIN UPDATE agent_profiles SET updated_at=now() WHERE agent_id=personal_agent;
    EXCEPTION WHEN raise_exception THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'unchanged revision write accepted'; END IF;
    rejected := false;
    BEGIN UPDATE agent_profiles SET created_at=created_at+interval '1 second',profile_version=2 WHERE agent_id=personal_agent;
    EXCEPTION WHEN raise_exception THEN rejected := true; END;
    IF NOT rejected THEN RAISE EXCEPTION 'creation time was overwritten'; END IF;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=personal_agent AND profile_version=1;
    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected<>1 THEN RAISE EXCEPTION 'initial CAS failed'; END IF;
    UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=personal_agent AND profile_version=1;
    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected<>0 THEN RAISE EXCEPTION 'stale revision wrote again'; END IF;
    IF NOT EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=personal_agent AND profile_version=2 AND updated_at>=created_at) THEN
        RAISE EXCEPTION 'valid monotonic revision failed';
    END IF;
    DELETE FROM agents WHERE id IN (personal_agent,organization_agent,business_agent,system_agent);
    IF EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id IN (personal_agent,organization_agent,business_agent,system_agent)) THEN
        RAISE EXCEPTION 'Agent deletion left dangling profile';
    END IF;
    DELETE FROM accounts WHERE id IN (person_a,person_b,org_owner,business_owner);
END $$;
