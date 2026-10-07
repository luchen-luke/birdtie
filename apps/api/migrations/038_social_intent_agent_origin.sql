BEGIN;

-- A task can become a social declaration only after the person explicitly
-- confirms an edited private draft. Existing tasks are never backfilled.
ALTER TABLE social_intents ADD COLUMN source_agent_task_id uuid
    REFERENCES agent_tasks(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX social_intents_one_draft_per_agent_task
    ON social_intents(source_agent_task_id)
    WHERE source_agent_task_id IS NOT NULL;

CREATE OR REPLACE FUNCTION birdtie_social_intent_agent_origin_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.source_agent_task_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM agent_tasks t WHERE t.id=NEW.source_agent_task_id
          AND t.owner_account_id=NEW.creator_account_id
          AND t.acting_user_account_id=NEW.creator_account_id
          AND t.principal_type='person' AND t.intent='FIND_ACTIVITY'
          AND t.status='COMPLETED'
    ) THEN
        RAISE EXCEPTION 'social intent source must be an owned completed personal activity task';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER social_intent_agent_origin_guard
    BEFORE INSERT OR UPDATE OF source_agent_task_id,creator_account_id
    ON social_intents FOR EACH ROW EXECUTE FUNCTION birdtie_social_intent_agent_origin_guard();

COMMIT;
