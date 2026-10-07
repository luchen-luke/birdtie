BEGIN;
DO $$ BEGIN
    -- Tombstones and inferred shape records are data too. Down never becomes an
    -- implicit purge just because payloads are empty, expired or unconsumed.
    IF EXISTS(SELECT 1 FROM agent_memories) THEN
        RAISE EXCEPTION '056 down refuses to erase AgentMemory data or controls';
    END IF;
END $$;
DROP TABLE agent_memories;
DROP FUNCTION birdtie_guard_agent_memory();
DROP FUNCTION birdtie_agent_memory_value_valid(jsonb);
DROP FUNCTION birdtie_agent_memory_compact_json(jsonb,integer);
DROP FUNCTION birdtie_agent_memory_text_valid(text,boolean);
-- Stable identities, native Profile versions and old source rows are untouched.
COMMIT;
