BEGIN;
CREATE TABLE IF NOT EXISTS incident_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
    target_type text NOT NULL CHECK (target_type IN ('activity','organization','account','general')),
    target_id uuid,
    reason text NOT NULL CHECK (reason IN ('safety','harassment','incorrect_information','technical','other')),
    details text NOT NULL CHECK (length(trim(details)) BETWEEN 10 AND 1000),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','reviewing','resolved')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((target_type='general' AND target_id IS NULL) OR
           (target_type<>'general' AND target_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS incident_reports_owner_recent
    ON incident_reports (reporter_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS incident_reports_open_recent
    ON incident_reports (created_at, id) WHERE status='open';

CREATE TABLE IF NOT EXISTS admin_audit_events (
    id bigserial PRIMARY KEY,
    actor_account_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    resource_type text NOT NULL CHECK (resource_type IN ('organization','activity','faq')),
    resource_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN
        ('organization_create','organization_profile_update','activity_create','activity_update',
         'activity_publish','activity_cancel','faq_create','faq_update','faq_delete')),
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS admin_audit_by_organization
    ON admin_audit_events (organization_id, occurred_at DESC, id DESC);
COMMIT;
