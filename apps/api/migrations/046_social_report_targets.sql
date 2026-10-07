BEGIN;

ALTER TABLE incident_reports DROP CONSTRAINT incident_reports_target_type_check;
ALTER TABLE incident_reports ADD CONSTRAINT incident_reports_target_type_check
    CHECK (target_type IN
        ('activity','organization','account','message','community','business','general'));

COMMIT;
