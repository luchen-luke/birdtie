BEGIN;
DROP TRIGGER memory_candidate_evidence_removed ON agent_memory_evidence;
DROP TRIGGER memory_candidate_memory_changed ON agent_memories;
DROP FUNCTION birdtie_supersede_memory_candidates();
DROP TABLE agent_memory_candidates;
DROP FUNCTION birdtie_guard_memory_candidate();
COMMIT;
