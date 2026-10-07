BEGIN;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM contexts WHERE context_type<>'CITY') OR
       EXISTS (SELECT 1 FROM person_contexts) OR
       EXISTS (SELECT 1 FROM agent_tasks WHERE context_type<>'CITY'
           OR city_id IS NULL OR city_context_id IS NULL) THEN
        RAISE EXCEPTION 'cannot remove Context Graph while new context data exists';
    END IF;
END $$;
DROP TRIGGER agent_task_context_validate ON agent_tasks;
DROP FUNCTION birdtie_validate_agent_task_context();
DROP INDEX agent_tasks_context_recent;
ALTER TABLE agent_tasks DROP CONSTRAINT agent_task_context_shape;
ALTER TABLE agent_tasks DROP COLUMN context_id;
ALTER TABLE agent_tasks DROP COLUMN context_type;
ALTER TABLE agent_tasks ALTER COLUMN city_id SET NOT NULL;
ALTER TABLE agent_tasks ALTER COLUMN city_context_id SET NOT NULL;
DROP TABLE person_contexts;
DROP FUNCTION birdtie_person_context_requires_person();
DROP TRIGGER city_context_create_node ON city_contexts;
DROP FUNCTION birdtie_create_city_context_node();
DROP TABLE contexts;
COMMIT;
