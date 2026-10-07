BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM business_review_grants) OR EXISTS(SELECT 1 FROM business_claim_controls) OR
    EXISTS(SELECT 1 FROM business_console_profiles) OR EXISTS(SELECT 1 FROM business_console_venue_facts) OR
    EXISTS(SELECT 1 FROM business_console_membership_controls) OR EXISTS(SELECT 1 FROM business_console_audit_events)
 THEN RAISE EXCEPTION 'business console history must be preserved; refusing nonempty down'; END IF;
END $$;
DROP TABLE business_console_audit_events;
DROP TABLE business_console_membership_controls;
DROP TABLE business_console_venue_facts;
DROP TABLE business_console_profiles;
DROP TABLE business_claim_controls;
DROP TABLE business_review_grants;
DROP FUNCTION birdtie_business_console_person_guard();
DROP FUNCTION birdtie_business_console_version_guard();
DROP FUNCTION birdtie_business_venue_facts_valid(jsonb);
DROP FUNCTION birdtie_business_profile_valid(jsonb);
DROP FUNCTION birdtie_business_https_valid(text);
CREATE OR REPLACE FUNCTION birdtie_validate_business_member() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.user_account_id
                  AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Business membership requires active Person';
    END IF;
    RETURN NEW;
END $$;
COMMIT;
