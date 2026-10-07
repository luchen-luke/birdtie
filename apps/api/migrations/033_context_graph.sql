BEGIN;

-- Contexts are descriptive nodes, never accounts or Agents. Non-city nodes
-- have no implicit city parent or public-visibility guarantee.
CREATE TABLE contexts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    context_type text NOT NULL CHECK (context_type IN
        ('CITY','COUNTRY','INSTITUTION','COMMUNITY','ONLINE')),
    city_id text REFERENCES city_contexts(city_id) ON DELETE RESTRICT,
    country_code text,
    institution_key text,
    community_id uuid REFERENCES communities(id) ON DELETE RESTRICT,
    online_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT contexts_shape CHECK (
        (context_type='CITY' AND city_id IS NOT NULL AND country_code IS NULL
            AND institution_key IS NULL AND community_id IS NULL AND online_key IS NULL) OR
        (context_type='COUNTRY' AND city_id IS NULL AND country_code IS NOT NULL
            AND country_code ~ '^[A-Z]{2}$'
            AND institution_key IS NULL AND community_id IS NULL AND online_key IS NULL) OR
        (context_type='INSTITUTION' AND city_id IS NULL AND country_code IS NULL
            AND institution_key IS NOT NULL AND length(trim(institution_key)) BETWEEN 1 AND 160
            AND community_id IS NULL AND online_key IS NULL) OR
        (context_type='COMMUNITY' AND city_id IS NULL AND country_code IS NULL
            AND institution_key IS NULL AND community_id IS NOT NULL AND online_key IS NULL) OR
        (context_type='ONLINE' AND city_id IS NULL AND country_code IS NULL
            AND institution_key IS NULL AND community_id IS NULL
            AND online_key IS NOT NULL AND length(trim(online_key)) BETWEEN 1 AND 160)
    ),
    UNIQUE (context_type,id)
);
CREATE UNIQUE INDEX contexts_one_city ON contexts(city_id) WHERE city_id IS NOT NULL;
CREATE UNIQUE INDEX contexts_one_country ON contexts(country_code) WHERE country_code IS NOT NULL;
CREATE UNIQUE INDEX contexts_one_institution ON contexts(institution_key)
    WHERE institution_key IS NOT NULL;
CREATE UNIQUE INDEX contexts_one_community ON contexts(community_id)
    WHERE community_id IS NOT NULL;
CREATE UNIQUE INDEX contexts_one_online ON contexts(online_key) WHERE online_key IS NOT NULL;

-- An association is a private declaration, not proof of residence,
-- institution membership or Community membership. No backfill from City.
CREATE TABLE person_contexts (
    person_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    context_id uuid NOT NULL REFERENCES contexts(id) ON DELETE RESTRICT,
    relation text NOT NULL CHECK (relation IN
        ('current','home','past','destination','affiliation','interest')),
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private','public')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (person_account_id,context_id,relation)
);
CREATE INDEX person_contexts_context ON person_contexts(context_id,relation,person_account_id);
CREATE OR REPLACE FUNCTION birdtie_person_context_requires_person() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts
        WHERE id=NEW.person_account_id AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'person context requires active person account';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER person_context_requires_person BEFORE INSERT OR UPDATE
    ON person_contexts FOR EACH ROW EXECUTE FUNCTION birdtie_person_context_requires_person();

-- Legacy city IDs remain stable; the new UUID identifies a Context node.
INSERT INTO contexts(context_type,city_id)
SELECT 'CITY',city_id FROM city_contexts ON CONFLICT DO NOTHING;
CREATE OR REPLACE FUNCTION birdtie_create_city_context_node() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO contexts(context_type,city_id)
    VALUES('CITY',NEW.city_id) ON CONFLICT DO NOTHING;
    RETURN NEW;
END $$;
CREATE TRIGGER city_context_create_node AFTER INSERT ON city_contexts
    FOR EACH ROW EXECUTE FUNCTION birdtie_create_city_context_node();

ALTER TABLE agent_tasks ADD COLUMN context_type text NOT NULL DEFAULT 'CITY'
    CHECK (context_type IN ('CITY','COUNTRY','INSTITUTION','COMMUNITY','ONLINE'));
ALTER TABLE agent_tasks ADD COLUMN context_id uuid REFERENCES contexts(id) ON DELETE RESTRICT;
UPDATE agent_tasks t SET context_id=c.id
FROM contexts c WHERE c.context_type='CITY' AND c.city_id=t.city_id;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_tasks WHERE context_id IS NULL) THEN
        RAISE EXCEPTION 'unmapped legacy Agent task city';
    END IF;
END $$;
ALTER TABLE agent_tasks ALTER COLUMN city_id DROP NOT NULL;
ALTER TABLE agent_tasks ALTER COLUMN city_context_id DROP NOT NULL;
ALTER TABLE agent_tasks ADD CONSTRAINT agent_task_context_shape CHECK (
    (context_type='CITY' AND city_id IS NOT NULL AND city_context_id IS NOT NULL
        AND city_context_id=city_id) OR
    (context_type<>'CITY' AND city_id IS NULL AND city_context_id IS NULL)
);
CREATE INDEX agent_tasks_context_recent ON agent_tasks(context_type,context_id,updated_at DESC);

-- Derive a Context for old clients that still write only city_id. For new
-- clients, validate the typed reference and prevent mismatched city links.
CREATE OR REPLACE FUNCTION birdtie_validate_agent_task_context() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_type text; target_city text;
BEGIN
    IF NEW.context_type='CITY' AND NEW.context_id IS NULL AND NEW.city_id IS NOT NULL THEN
        SELECT id INTO NEW.context_id FROM contexts
        WHERE context_type='CITY' AND city_id=NEW.city_id;
    END IF;
    SELECT context_type,city_id INTO target_type,target_city
    FROM contexts WHERE id=NEW.context_id;
    IF target_type IS NULL OR target_type<>NEW.context_type THEN
        RAISE EXCEPTION 'Agent task context type/ID mismatch';
    END IF;
    IF NEW.context_type='CITY' AND target_city IS DISTINCT FROM NEW.city_id THEN
        RAISE EXCEPTION 'Agent task CityContext does not match city_id';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER agent_task_context_validate BEFORE INSERT OR UPDATE OF
    context_type,context_id,city_id,city_context_id ON agent_tasks
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_agent_task_context();
ALTER TABLE agent_tasks ALTER COLUMN context_id SET NOT NULL;

COMMIT;
