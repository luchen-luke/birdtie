BEGIN;
DROP INDEX IF EXISTS organization_memberships_invited;
ALTER TABLE admin_audit_events DROP COLUMN IF EXISTS details;
ALTER TABLE admin_audit_events DROP CONSTRAINT IF EXISTS admin_audit_events_action_check;
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_events_action_check
    CHECK (action IN
        ('organization_create','organization_profile_update','activity_create','activity_update',
         'activity_publish','activity_cancel','faq_create','faq_update','faq_delete'));
ALTER TABLE admin_audit_events DROP CONSTRAINT IF EXISTS admin_audit_events_resource_type_check;
ALTER TABLE admin_audit_events ADD CONSTRAINT admin_audit_events_resource_type_check
    CHECK (resource_type IN ('organization','activity','faq'));
-- This explicit restoration fails if multiple active owners exist; ownership
-- must first be consolidated intentionally.
CREATE UNIQUE INDEX organization_one_active_owner
    ON organization_memberships (organization_id)
    WHERE role='owner' AND status='active';
COMMIT;
