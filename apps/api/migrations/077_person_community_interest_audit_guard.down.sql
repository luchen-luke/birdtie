BEGIN;
LOCK TABLE audit_events IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM audit_events WHERE resource_type='person_community_declaration' OR purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION') THEN RAISE EXCEPTION 'community interest audit history prevents downgrade'; END IF; END $$;
DROP INDEX community_interest_audit_epoch;
DROP TRIGGER community_interest_audit_truncate_guard ON audit_events;
DROP TRIGGER community_interest_audit_guard ON audit_events;
DROP FUNCTION birdtie_community_interest_audit_guard();
COMMIT;
