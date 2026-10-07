BEGIN;

-- A separate Person-only private authority, never joined into public profile
-- projections. No preferences, context history, analysis grant or content is
-- copied from existing public/private UserProfile or declarations.
ALTER TABLE agent_profiles ADD CONSTRAINT agent_profiles_private_binding
    UNIQUE(agent_id,owner_id,owner_type);
CREATE TABLE agent_private_profiles (
    agent_id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
    fields jsonb NOT NULL CHECK(jsonb_typeof(fields)='object' AND octet_length(fields::text)<=16384),
    written_profile_version bigint NOT NULL CHECK(written_profile_version>1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_private_profile_person_binding FOREIGN KEY(agent_id,owner_id,owner_type)
        REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
    CONSTRAINT agent_private_profile_finite_times CHECK (
        isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at
    )
);

CREATE FUNCTION birdtie_guard_agent_private_profile() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    current_version bigint;
    field_name text;
    item jsonb;
    target_agent uuid;
BEGIN
    target_agent := CASE WHEN TG_OP='DELETE' THEN OLD.agent_id ELSE NEW.agent_id END;
    SELECT profile_version INTO current_version FROM agent_profiles
        WHERE agent_id=target_agent FOR KEY SHARE;
    IF TG_OP='DELETE' THEN
        -- Parent deletion invalidates identity/source and may cascade. Otherwise
        -- an explicit clear must advance the same aggregate source revision.
        IF current_version IS NOT NULL AND current_version<=OLD.written_profile_version THEN
            RAISE EXCEPTION 'private profile clear requires a newer authoritative revision';
        END IF;
        RETURN OLD;
    END IF;
    IF current_version IS NULL OR NEW.written_profile_version IS DISTINCT FROM current_version THEN
        RAISE EXCEPTION 'private profile write requires current authoritative revision';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.agent_id IS DISTINCT FROM OLD.agent_id OR NEW.owner_id IS DISTINCT FROM OLD.owner_id OR
           NEW.owner_type IS DISTINCT FROM OLD.owner_type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
            RAISE EXCEPTION 'private profile binding is immutable';
        END IF;
        IF NEW.written_profile_version<=OLD.written_profile_version THEN
            RAISE EXCEPTION 'private profile content cannot reuse an old revision';
        END IF;
        NEW.updated_at := GREATEST(clock_timestamp(),OLD.updated_at);
    END IF;
    IF NEW.fields - ARRAY['personalPreferences','socialPreferences','preferredActivityTypes',
        'travelPreferences','interactionPreferences','languagePreferences','availability',
        'privateCityHistory','agentNotes']::text[] <> '{}'::jsonb OR
        (SELECT count(*) FROM jsonb_object_keys(NEW.fields))<>9 THEN
        RAISE EXCEPTION 'private profile requires only the nine declared fields';
    END IF;
    FOREACH field_name IN ARRAY ARRAY['personalPreferences','socialPreferences','preferredActivityTypes',
        'travelPreferences','interactionPreferences','languagePreferences'] LOOP
        IF jsonb_typeof(NEW.fields->field_name) IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'private profile list has invalid shape';
        END IF;
        IF jsonb_array_length(NEW.fields->field_name)>20 THEN RAISE EXCEPTION 'private profile list too long'; END IF;
        FOR item IN SELECT value FROM jsonb_array_elements(NEW.fields->field_name) LOOP
            IF jsonb_typeof(item) IS DISTINCT FROM 'string' OR length(item#>>'{}') NOT BETWEEN 1 AND 160 THEN
                RAISE EXCEPTION 'private profile list item has invalid shape';
            END IF;
        END LOOP;
    END LOOP;
    FOREACH field_name IN ARRAY ARRAY['availability','privateCityHistory','agentNotes'] LOOP
        IF jsonb_typeof(NEW.fields->field_name) IS DISTINCT FROM 'string' OR length(NEW.fields->>field_name)>2000 THEN
            RAISE EXCEPTION 'private profile description has invalid shape';
        END IF;
    END LOOP;
    RETURN NEW;
END $$;
CREATE TRIGGER agent_private_profile_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_private_profiles
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_private_profile();

COMMIT;
