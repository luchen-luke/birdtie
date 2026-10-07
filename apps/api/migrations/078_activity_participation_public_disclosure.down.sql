BEGIN;
LOCK TABLE activity_participations,audit_events IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM activity_participations WHERE disclosure_revision<>1 OR disclosure_visibility<>'private') OR EXISTS(SELECT 1 FROM audit_events WHERE resource_type='activity_participation_disclosure' OR purpose='HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE') THEN RAISE EXCEPTION 'used participation disclosure lineage prevents down'; END IF;
END $$;
DROP FUNCTION birdtie_participation_public_source(uuid,uuid,timestamptz);
DROP TRIGGER participation_disclosure_version ON activity_participations;
DROP FUNCTION birdtie_participation_disclosure_version();
DROP TRIGGER participation_disclosure_audit_guard ON audit_events;
DROP TRIGGER participation_disclosure_audit_truncate_guard ON audit_events;
DROP FUNCTION birdtie_participation_disclosure_audit_guard();
DROP INDEX participation_disclosure_audit_epoch;
ALTER TABLE activity_participations DROP CONSTRAINT participation_disclosure_shape;
ALTER TABLE activity_participations DROP COLUMN disclosure_revision,DROP COLUMN disclosure_visibility,DROP COLUMN disclosure_approved_at,DROP COLUMN disclosure_expires_at,DROP COLUMN disclosure_source_revision,DROP COLUMN disclosure_activity_revision,DROP COLUMN disclosure_source_digest;
COMMIT;
