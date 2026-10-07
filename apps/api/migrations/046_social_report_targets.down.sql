BEGIN;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM incident_reports
        WHERE target_type IN ('message','community','business')) THEN
        RAISE EXCEPTION 'cannot remove social report target types while reports exist';
    END IF;
END $$;
ALTER TABLE incident_reports DROP CONSTRAINT incident_reports_target_type_check;
ALTER TABLE incident_reports ADD CONSTRAINT incident_reports_target_type_check
    CHECK (target_type IN ('activity','organization','account','general'));

COMMIT;
