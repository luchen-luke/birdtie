-- Run against the local development seed with psql -v ON_ERROR_STOP=1.
-- All changes are rolled back.
BEGIN;
DO $$
DECLARE
    activity_id uuid;
    first_person uuid;
    second_person uuid;
    rejected boolean := false;
BEGIN
    SELECT id INTO activity_id FROM activities
    WHERE publication_status = 'published' AND visibility = 'public'
      AND cancelled_at IS NULL AND ends_at > now()
    LIMIT 1;
    IF activity_id IS NULL THEN
        RAISE EXCEPTION 'need a future published development Activity';
    END IF;
    INSERT INTO accounts (id, account_type)
    VALUES (gen_random_uuid(), 'person') RETURNING id INTO first_person;
    INSERT INTO accounts (id, account_type)
    VALUES (gen_random_uuid(), 'person') RETURNING id INTO second_person;
    INSERT INTO activity_participations (activity_id, participant_account_id)
    VALUES (activity_id, first_person);
    UPDATE activities SET cancelled_at = now() WHERE id = activity_id;
    BEGIN
        INSERT INTO activity_participations (activity_id, participant_account_id)
        VALUES (activity_id, second_person);
    EXCEPTION WHEN check_violation THEN
        rejected := true;
    END;
    IF NOT rejected THEN
        RAISE EXCEPTION 'cancelled Activity accepted a new participation';
    END IF;
END $$;
ROLLBACK;
