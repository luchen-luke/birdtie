BEGIN;
-- Transfers require a new owner to be active before the previous owner leaves.
DROP INDEX IF EXISTS organization_one_active_owner;
ALTER TABLE admin_audit_events
    DROP CONSTRAINT IF EXISTS admin_audit_events_resource_type_check;
ALTER TABLE admin_audit_events
    ADD CONSTRAINT admin_audit_events_resource_type_check
    CHECK (resource_type IN ('organization','activity','faq','membership'));
ALTER TABLE admin_audit_events
    DROP CONSTRAINT IF EXISTS admin_audit_events_action_check;
ALTER TABLE admin_audit_events
    ADD CONSTRAINT admin_audit_events_action_check
    CHECK (action IN
        ('organization_create','organization_profile_update','activity_create','activity_update',
         'activity_publish','activity_cancel','faq_create','faq_update','faq_delete',
         'membership_invite','membership_accept','membership_role_change','membership_revoke'));
ALTER TABLE admin_audit_events
    ADD COLUMN IF NOT EXISTS details jsonb NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX IF NOT EXISTS organization_memberships_invited
    ON organization_memberships (user_account_id, organization_id)
    WHERE status='invited';
COMMIT;
