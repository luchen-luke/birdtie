BEGIN;
-- Metadata initialization only. Original019 uniqueness and original053
-- suspended Business/Profile bootstrap remain authoritative; no activation.
ALTER TABLE business_console_audit_events DROP CONSTRAINT business_console_audit_events_action_check;
ALTER TABLE business_console_audit_events ADD CONSTRAINT business_console_audit_events_action_check CHECK(action IN(
 'claim_submit','claim_approve','claim_reject','claim_revoke',
 'profile_submit','profile_approve','profile_reject','profile_revoke',
 'venue_submit','venue_approve','venue_reject','venue_revoke',
 'member_grant','member_remove','member_transfer_owner','agent_provision'));
COMMIT;
