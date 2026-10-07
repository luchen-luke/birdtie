-- Synthetic organizer checks; no records survive this transaction.
BEGIN;
DO $$
DECLARE v_person uuid; v_city text; v_community uuid; v_person_activity uuid; v_community_activity uuid;
BEGIN
    SELECT id INTO v_person FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1;
    SELECT id INTO v_city FROM cities WHERE publication_status='published' ORDER BY id LIMIT 1;
    IF v_person IS NULL OR v_city IS NULL THEN RAISE EXCEPTION 'person and published city required'; END IF;
    INSERT INTO communities(city_id,owner_account_id,name,summary,visibility,publication_status,
        owner_confirmed_at,source_label,source_ref,maintainer_label)
    VALUES(v_city,v_person,'Organizer synthetic','','private','published',now(),
        'test','synthetic:organizer','test') RETURNING id INTO v_community;
    INSERT INTO activities(host_account_id,host_label,city_id,title,summary,starts_at,ends_at,time_zone,
        source_label,source_ref,maintainer_label)
    VALUES(v_person,'Synthetic Person',v_city,'Person organizer synthetic','',now()+interval '1 day',
        now()+interval '1 day 2 hours','Europe/London','test','synthetic:person','test')
    RETURNING id INTO v_person_activity;
    IF NOT EXISTS(SELECT 1 FROM activity_organizers WHERE activity_id=v_person_activity
        AND person_account_id=v_person AND community_id IS NULL AND organization_id IS NULL) THEN
        RAISE EXCEPTION 'person organizer missing';
    END IF;
    INSERT INTO activities(host_account_id,host_label,city_id,title,summary,starts_at,ends_at,time_zone,
        source_label,source_ref,maintainer_label)
    VALUES(v_person,'Synthetic Community',v_city,'Community organizer synthetic','',now()+interval '2 days',
        now()+interval '2 days 2 hours','Europe/London','test','synthetic:community-activity','test')
    RETURNING id INTO v_community_activity;
    UPDATE activity_organizers SET person_account_id=NULL,community_id=v_community
    WHERE activity_id=v_community_activity;
    IF NOT EXISTS(SELECT 1 FROM activity_organizers WHERE activity_id=v_community_activity
        AND person_account_id IS NULL AND community_id=v_community AND organization_id IS NULL) THEN
        RAISE EXCEPTION 'community organizer missing';
    END IF;
END $$;
ROLLBACK;
