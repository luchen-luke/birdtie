BEGIN;
LOCK TABLE agent_memory_evidence IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_memory_evidence WHERE owner_type='ORGANIZATION') THEN
 RAISE EXCEPTION '073 down refuses Organization Evidence, including removed history'; END IF;
END $$;
ALTER TABLE agent_memory_evidence DROP CONSTRAINT memory_evidence_shape;
ALTER TABLE agent_memory_evidence ADD CONSTRAINT memory_evidence_shape CHECK (coalesce((
      (status='REMOVED' AND source_type IS NULL AND source_id IS NULL AND source_version_kind IS NULL
        AND source_revision IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL)
      OR (status='CURRENT' AND source_id IS NOT NULL AND source_id<>'00000000-0000-0000-0000-000000000000'::uuid
        AND signal_type='MANUAL_REFERENCE' AND weight=1 AND event_time IS NOT NULL
        AND ((source_type='MOMENT' AND source_version_kind='REVISION' AND source_revision>0 AND source_token IS NULL)
          OR (source_type='ACTIVITY_PARTICIPATION' AND source_version_kind='UPDATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')
          OR (source_type='SAVED_PLACE' AND source_version_kind='CREATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')))
    ),false));
ALTER TABLE agent_memory_evidence DROP CONSTRAINT agent_memory_evidence_owner_type_check;
ALTER TABLE agent_memory_evidence ADD CONSTRAINT agent_memory_evidence_owner_type_check CHECK(owner_type='PERSON');
COMMIT;
