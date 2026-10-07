BEGIN;
DROP TRIGGER memory_reinforcement_evidence_removed ON agent_memory_evidence;
DROP TRIGGER memory_reinforcement_memory_changed ON agent_memories;
DROP FUNCTION birdtie_clear_memory_reinforcement();
DROP TABLE agent_memory_reinforcement;
DROP FUNCTION birdtie_guard_memory_reinforcement();
COMMIT;
