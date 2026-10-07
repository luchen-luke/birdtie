BEGIN;

-- Only version-bound metadata for an owner's manual references is stored.
-- This schema does not install inference, source analysis or model permission.
ALTER TABLE agent_memories ADD CONSTRAINT agent_memory_evidence_binding
    UNIQUE(id,agent_id,owner_type,owner_id);
CREATE TABLE agent_memory_evidence (
    id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'::uuid),
    schema_version text NOT NULL DEFAULT 'agent-memory-evidence-v1' CHECK(schema_version='agent-memory-evidence-v1'),
    memory_id uuid NOT NULL,
    memory_version bigint NOT NULL CHECK(memory_version>0),
    agent_id uuid NOT NULL,
    owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
    owner_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    status text NOT NULL DEFAULT 'CURRENT' CHECK(status IN ('CURRENT','REMOVED')),
    source_type text,
    source_id uuid,
    source_version_kind text,
    source_revision bigint,
    source_token text,
    signal_type text,
    weight double precision NOT NULL DEFAULT 1,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    event_time timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(memory_id,agent_id,owner_type,owner_id)
        REFERENCES agent_memories(id,agent_id,owner_type,owner_id) ON DELETE CASCADE,
    CONSTRAINT memory_evidence_shape CHECK (coalesce((
      (status='REMOVED' AND source_type IS NULL AND source_id IS NULL AND source_version_kind IS NULL
        AND source_revision IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL)
      OR (status='CURRENT' AND source_id IS NOT NULL AND source_id<>'00000000-0000-0000-0000-000000000000'::uuid
        AND signal_type='MANUAL_REFERENCE' AND weight=1 AND event_time IS NOT NULL
        AND ((source_type='MOMENT' AND source_version_kind='REVISION' AND source_revision>0 AND source_token IS NULL)
          OR (source_type='ACTIVITY_PARTICIPATION' AND source_version_kind='UPDATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')
          OR (source_type='SAVED_PLACE' AND source_version_kind='CREATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')))
    ),false)),
    CONSTRAINT memory_evidence_times CHECK (
      isfinite(observed_at) AND isfinite(created_at) AND isfinite(updated_at)
      AND observed_at>='0001-01-01 00:00:00+00'::timestamptz AND observed_at<'10000-01-01 00:00:00+00'::timestamptz
      AND created_at>=observed_at AND updated_at>=created_at AND updated_at<'10000-01-01 00:00:00+00'::timestamptz
      AND (event_time IS NULL OR (isfinite(event_time) AND event_time>='0001-01-01 00:00:00+00'::timestamptz AND event_time<=observed_at))
    )
);
CREATE UNIQUE INDEX memory_evidence_current_source ON agent_memory_evidence(memory_id,memory_version,source_type,source_id)
    WHERE status='CURRENT';
CREATE INDEX memory_evidence_owner ON agent_memory_evidence(owner_id,memory_id,observed_at,id);

CREATE FUNCTION birdtie_guard_memory_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF EXISTS(SELECT 1 FROM agent_memories WHERE id=OLD.memory_id FOR KEY SHARE) THEN
            RAISE EXCEPTION 'Evidence removal requires a tombstone';
        END IF;
        RETURN OLD;
    ELSIF TG_OP='INSERT' THEN
        IF NEW.version<>1 OR NEW.status<>'CURRENT' THEN RAISE EXCEPTION 'new Evidence must be current version one'; END IF;
        IF NOT EXISTS(SELECT 1 FROM agent_memories m WHERE m.id=NEW.memory_id AND m.agent_id=NEW.agent_id
            AND m.owner_id=NEW.owner_id AND m.owner_type=NEW.owner_type AND m.version=NEW.memory_version
            AND m.source_type='EXPLICIT' AND m.status='ACTIVE' AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp() FOR SHARE) THEN
            RAISE EXCEPTION 'Evidence requires the current explicit Memory';
        END IF;
    ELSE
        IF NEW.id IS DISTINCT FROM OLD.id OR NEW.memory_id IS DISTINCT FROM OLD.memory_id OR
           NEW.memory_version IS DISTINCT FROM OLD.memory_version OR NEW.agent_id IS DISTINCT FROM OLD.agent_id OR
           NEW.owner_type IS DISTINCT FROM OLD.owner_type OR NEW.owner_id IS DISTINCT FROM OLD.owner_id OR
           NEW.schema_version IS DISTINCT FROM OLD.schema_version OR NEW.created_at IS DISTINCT FROM OLD.created_at OR
           NEW.observed_at IS DISTINCT FROM OLD.observed_at THEN RAISE EXCEPTION 'Evidence binding is immutable'; END IF;
        IF OLD.status<>'CURRENT' OR NEW.status<>'REMOVED' OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'Evidence can only be removed exactly once';
        END IF;
        NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER memory_evidence_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_memory_evidence
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_memory_evidence();

-- A changed declaration cannot retain the old version's provenance. Scrub
-- references in the same Memory transaction, including the ordinary delete API.
CREATE FUNCTION birdtie_invalidate_memory_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE agent_memory_evidence SET status='REMOVED',version=version+1,source_type=NULL,source_id=NULL,
        source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL
        WHERE memory_id=NEW.id AND status='CURRENT';
    RETURN NEW;
END $$;
CREATE TRIGGER memory_evidence_memory_changed AFTER UPDATE OF version ON agent_memories
    FOR EACH ROW WHEN(OLD.version IS DISTINCT FROM NEW.version) EXECUTE FUNCTION birdtie_invalidate_memory_evidence();
COMMIT;
