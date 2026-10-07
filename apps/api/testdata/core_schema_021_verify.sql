-- Apply migration 021 first. This test rolls back all of its fixture rows.
BEGIN;
DO $$
DECLARE
    person_id uuid := gen_random_uuid();
    organization_account_id uuid := gen_random_uuid();
    organization_id uuid;
    test_activity_id uuid;
    rejected boolean;
BEGIN
    INSERT INTO accounts (id, account_type) VALUES (person_id, 'person');
    INSERT INTO accounts (id, account_type) VALUES (organization_account_id, 'organization');
    INSERT INTO organizations (account_id, organization_type, name, slug)
        VALUES (organization_account_id, 'club', 'Schema test club', 'schema-test-club')
        RETURNING id INTO organization_id;
    INSERT INTO activities (
        host_account_id, organization_id, created_by_account_id,
        city_id, title, host_label, starts_at, ends_at, time_zone,
        publication_status, published_at, source_label, source_ref, maintainer_label
    ) VALUES (
        organization_account_id, organization_id, person_id,
        'aberdeen-gb', 'Schema test event', 'Schema test club',
        now() + interval '1 day', now() + interval '2 days', 'Europe/London',
        'published', now(), 'Schema test', 'test://schema-021', 'Schema test'
    ) RETURNING id INTO test_activity_id;

    rejected := false;
    BEGIN
        INSERT INTO activities (
            host_account_id, organization_id, city_id, title, host_label,
            starts_at, ends_at, time_zone, source_label, source_ref, maintainer_label
        ) VALUES (
            person_id, organization_id, 'aberdeen-gb', 'Wrong host', 'Wrong host',
            now() + interval '1 day', now() + interval '2 days', 'Europe/London',
            'Schema test', 'test://schema-021', 'Schema test'
        );
    EXCEPTION WHEN OTHERS THEN rejected := true;
    END;
    IF NOT rejected THEN RAISE EXCEPTION 'organization activity accepted wrong host'; END IF;

    rejected := false;
    BEGIN
        UPDATE activities SET publication_status = 'draft', cancelled_at = now()
            WHERE id = test_activity_id;
    EXCEPTION WHEN check_violation THEN rejected := true;
    END;
    IF NOT rejected THEN RAISE EXCEPTION 'cancelled draft activity accepted'; END IF;

    INSERT INTO activity_participations (activity_id, participant_account_id)
        VALUES (test_activity_id, person_id);
    rejected := false;
    BEGIN
        INSERT INTO activity_participations (activity_id, participant_account_id)
            VALUES (test_activity_id, person_id);
    EXCEPTION WHEN unique_violation THEN rejected := true;
    END;
    IF NOT rejected THEN RAISE EXCEPTION 'duplicate RSVP accepted'; END IF;

    rejected := false;
    BEGIN
        INSERT INTO activity_participations (activity_id, participant_account_id, status)
            VALUES (test_activity_id, organization_account_id, 'going');
    EXCEPTION WHEN OTHERS THEN rejected := true;
    END;
    IF NOT rejected THEN RAISE EXCEPTION 'organization account RSVP accepted'; END IF;

    rejected := false;
    BEGIN
        UPDATE activity_participations SET status = 'cancelled'
            WHERE activity_participations.activity_id = test_activity_id
              AND participant_account_id = person_id;
    EXCEPTION WHEN check_violation THEN rejected := true;
    END;
    IF NOT rejected THEN RAISE EXCEPTION 'cancelled RSVP without timestamp accepted'; END IF;

    UPDATE activity_participations SET status = 'cancelled', cancelled_at = now()
        WHERE activity_participations.activity_id = test_activity_id
          AND participant_account_id = person_id;
    IF NOT EXISTS (SELECT 1 FROM activity_participations
                   WHERE activity_participations.activity_id = test_activity_id
                     AND participant_account_id = person_id AND status = 'cancelled') THEN
        RAISE EXCEPTION 'cancelled RSVP did not persist';
    END IF;
END $$;
ROLLBACK;
