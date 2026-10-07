BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM agent_memory_evidence LIMIT 1) THEN
        RAISE EXCEPTION 'Refusing to discard MemoryEvidence or removal tombstones';
    END IF;
END $$;
DROP TRIGGER memory_evidence_memory_changed ON agent_memories;
DROP FUNCTION birdtie_invalidate_memory_evidence();
DROP TABLE agent_memory_evidence;
DROP FUNCTION birdtie_guard_memory_evidence();
ALTER TABLE agent_memories DROP CONSTRAINT agent_memory_evidence_binding;
COMMIT;
