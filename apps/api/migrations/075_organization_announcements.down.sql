BEGIN;
LOCK TABLE organization_announcements,organization_announcement_previews,organization_announcement_audit,organization_announcement_evidence_controls,agent_memory_evidence IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM organization_announcements) OR EXISTS(SELECT 1 FROM organization_announcement_previews)
 OR EXISTS(SELECT 1 FROM organization_announcement_audit) OR EXISTS(SELECT 1 FROM organization_announcement_evidence_controls)
 OR EXISTS(SELECT 1 FROM agent_memory_evidence WHERE source_type='ORGANIZATION_ANNOUNCEMENT') THEN
 RAISE EXCEPTION '075 down refuses announcement resources or evidence history'; END IF;
END $$;
DROP TRIGGER organization_announcement_evidence_control ON agent_memory_evidence;
DROP FUNCTION birdtie_organization_announcement_evidence_control();
DROP TABLE organization_announcement_evidence_controls;
DROP TABLE organization_announcement_audit;
DROP TABLE organization_announcement_previews;
DROP TABLE organization_announcements;
DROP FUNCTION birdtie_organization_announcement_guard();
ALTER TABLE agent_memory_evidence DROP CONSTRAINT memory_evidence_shape;
ALTER TABLE agent_memory_evidence ADD CONSTRAINT memory_evidence_shape CHECK (
 (owner_type='PERSON' AND (coalesce((
      (status='REMOVED' AND source_type IS NULL AND source_id IS NULL AND source_version_kind IS NULL
        AND source_revision IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL)
      OR (status='CURRENT' AND source_id IS NOT NULL AND source_id<>'00000000-0000-0000-0000-000000000000'::uuid
        AND signal_type='MANUAL_REFERENCE' AND weight=1 AND event_time IS NOT NULL
        AND ((source_type='MOMENT' AND source_version_kind='REVISION' AND source_revision>0 AND source_token IS NULL)
          OR (source_type='ACTIVITY_PARTICIPATION' AND source_version_kind='UPDATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')
          OR (source_type='SAVED_PLACE' AND source_version_kind='CREATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')))
    ),false))) OR
 (owner_type='ORGANIZATION' AND (coalesce((
 (status='REMOVED' AND source_type IS NULL AND source_id IS NULL AND source_version_kind IS NULL
 AND source_revision IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL)
 OR (status='CURRENT' AND source_id IS NOT NULL AND source_id<>'00000000-0000-0000-0000-000000000000'::uuid
 AND signal_type='MANUAL_REFERENCE' AND weight=1 AND event_time IS NOT NULL
 AND ((source_type IN ('ORGANIZATION_ACTIVITY','ORGANIZATION_ADMIN_INPUT') AND source_version_kind='REVISION' AND source_revision>0 AND source_token IS NULL
 AND (source_type<>'ORGANIZATION_ADMIN_INPUT' OR source_id<>memory_id))
 OR (source_type IN ('ORGANIZATION_PROFILE','ORGANIZATION_PUBLIC_FAQ') AND source_version_kind='UPDATED_AT_DIGEST'
 AND source_revision IS NULL AND source_token ~ '^[0-9a-f]{64}$')))
),false))));
COMMIT;
