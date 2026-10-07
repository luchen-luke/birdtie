BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM social_intents WHERE source_agent_task_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove social intent AgentTask links while user drafts exist';
    END IF;
END $$;
DROP TRIGGER social_intent_agent_origin_guard ON social_intents;
DROP FUNCTION birdtie_social_intent_agent_origin_guard();
DROP INDEX social_intents_one_draft_per_agent_task;
ALTER TABLE social_intents DROP COLUMN source_agent_task_id;
COMMIT;
