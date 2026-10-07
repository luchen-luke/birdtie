BEGIN;

-- Policy only: values remain in UserProfile / the owner's private content.
-- No inherited grants, Community principal, inferred audiences or model port.
CREATE TABLE agent_profile_field_visibility (
    agent_id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
    rules jsonb NOT NULL CHECK(jsonb_typeof(rules)='object' AND octet_length(rules::text)<=16384),
    written_profile_version bigint NOT NULL CHECK(written_profile_version>1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
    CHECK(isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at)
);

CREATE FUNCTION birdtie_guard_agent_profile_field_visibility() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    current_version bigint;
    target_agent uuid;
    rule_item record;
    community_item jsonb;
    community_count integer;
BEGIN
    target_agent := CASE WHEN TG_OP='DELETE' THEN OLD.agent_id ELSE NEW.agent_id END;
    SELECT profile_version INTO current_version FROM agent_profiles WHERE agent_id=target_agent FOR KEY SHARE;
    IF TG_OP='DELETE' THEN
        IF current_version IS NOT NULL AND current_version<=OLD.written_profile_version THEN
            RAISE EXCEPTION 'field policy clear requires newer authoritative revision';
        END IF;
        RETURN OLD;
    END IF;
    IF current_version IS NULL OR NEW.written_profile_version IS DISTINCT FROM current_version THEN
        RAISE EXCEPTION 'field policy requires current authoritative revision';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.agent_id IS DISTINCT FROM OLD.agent_id OR NEW.owner_id IS DISTINCT FROM OLD.owner_id OR
           NEW.owner_type IS DISTINCT FROM OLD.owner_type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
            RAISE EXCEPTION 'field policy binding is immutable';
        END IF;
        IF NEW.written_profile_version<=OLD.written_profile_version THEN
            RAISE EXCEPTION 'field policy cannot reuse previous revision';
        END IF;
        NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at);
    END IF;
    IF NEW.rules - ARRAY['displayName','bio','personalPreferences','socialPreferences','availability',
        'preferredActivityTypes','travelPreferences','interactionPreferences','privateCityHistory',
        'languagePreferences','agentNotes']::text[] <> '{}'::jsonb OR
        (SELECT count(*) FROM jsonb_object_keys(NEW.rules))<>11 THEN
        RAISE EXCEPTION 'field policy requires exactly current eleven fields';
    END IF;
    FOR rule_item IN SELECT key,value FROM jsonb_each(NEW.rules) LOOP
        IF jsonb_typeof(rule_item.value) IS DISTINCT FROM 'object' THEN
            RAISE EXCEPTION 'field policy rule must be an object';
        END IF;
        IF rule_item.value - ARRAY['visibility','communityIds']::text[] <> '{}'::jsonb OR
           (SELECT count(*) FROM jsonb_object_keys(rule_item.value))<>2 OR
           jsonb_typeof(rule_item.value->'visibility') IS DISTINCT FROM 'string' OR
           rule_item.value->>'visibility' NOT IN ('PUBLIC','CONNECTIONS','COMMUNITY','PRIVATE','AGENT_ONLY') OR
           jsonb_typeof(rule_item.value->'communityIds') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'field policy rule has invalid audience';
        END IF;
        community_count:=jsonb_array_length(rule_item.value->'communityIds');
        IF (rule_item.value->>'visibility'='COMMUNITY' AND community_count NOT BETWEEN 1 AND 8) OR
           (rule_item.value->>'visibility'<>'COMMUNITY' AND community_count<>0) THEN
            RAISE EXCEPTION 'field policy community scope must be explicit';
        END IF;
        IF (SELECT count(DISTINCT value#>>'{}') FROM jsonb_array_elements(rule_item.value->'communityIds'))<>community_count THEN
            RAISE EXCEPTION 'field policy community scope has duplicates';
        END IF;
        FOR community_item IN SELECT value FROM jsonb_array_elements(rule_item.value->'communityIds') LOOP
            IF jsonb_typeof(community_item) IS DISTINCT FROM 'string' OR
               community_item#>>'{}' !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
               community_item#>>'{}'='00000000-0000-0000-0000-000000000000' THEN
                RAISE EXCEPTION 'field policy community scope has invalid identity';
            END IF;
            IF NOT EXISTS(SELECT 1 FROM communities c JOIN community_memberships m ON m.community_id=c.id
                WHERE c.id=(community_item#>>'{}')::uuid AND c.lifecycle_status='active' AND c.publication_status='published'
                  AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
                  AND m.user_account_id=NEW.owner_id AND m.status='active') THEN
                RAISE EXCEPTION 'field policy owner must be current member of explicit community';
            END IF;
        END LOOP;
    END LOOP;
    RETURN NEW;
END $$;
CREATE TRIGGER agent_profile_field_visibility_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_profile_field_visibility
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_profile_field_visibility();

-- Additional field restriction only. Every caller must also preserve its
-- existing resource ACL. This function is not a model permission resolver.
CREATE FUNCTION birdtie_agent_profile_field_allowed(target uuid, viewer uuid, field_key text) RETURNS boolean
LANGUAGE plpgsql STABLE AS $$
DECLARE
    owner_kind text;
    field_rule jsonb;
    audience text;
BEGIN
    IF target IS NULL OR field_key IS NULL OR field_key NOT IN ('displayName','bio','personalPreferences','socialPreferences','availability',
        'preferredActivityTypes','travelPreferences','interactionPreferences','privateCityHistory',
        'languagePreferences','agentNotes') THEN RETURN false; END IF;
    SELECT account_type INTO owner_kind FROM accounts WHERE id=target AND status='active';
    IF owner_kind IS NULL THEN RETURN false; END IF;
    IF viewer IS NOT NULL AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=viewer AND status='active') THEN RETURN false; END IF;
    IF viewer IS NOT NULL AND EXISTS(SELECT 1 FROM account_blocks b
        WHERE (b.blocker_account_id=target AND b.blocked_account_id=viewer) OR
              (b.blocker_account_id=viewer AND b.blocked_account_id=target)) THEN RETURN false; END IF;
    IF owner_kind='person' AND NOT EXISTS(SELECT 1 FROM agents a JOIN agent_profiles p ON p.agent_id=a.id
        AND p.owner_id=target AND p.owner_type='PERSON'
        WHERE a.principal_account_id=target AND a.agent_type='personal' AND a.status='active') THEN RETURN false; END IF;
    IF viewer=target THEN RETURN true; END IF; -- Owner privacy control, not model authorization.
    SELECT v.rules->field_key INTO field_rule FROM agent_profile_field_visibility v
        WHERE v.owner_id=target AND v.owner_type='PERSON';
    IF field_rule IS NULL THEN
        RETURN field_key IN ('displayName','bio'); -- Existing source ACL still applies.
    END IF;
    audience:=field_rule->>'visibility';
    IF audience='PUBLIC' THEN RETURN true; END IF;
    IF audience='CONNECTIONS' THEN
        RETURN EXISTS(SELECT 1 FROM person_ties t JOIN connection_requests r ON r.id=t.request_id
            JOIN accounts a ON a.id=viewer AND a.account_type='person' AND a.status='active'
            WHERE t.status='active' AND t.person_a_account_id=LEAST(target,viewer) AND t.person_b_account_id=GREATEST(target,viewer)
              AND r.scope='friend' AND r.state='accepted'
              AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id
              AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id);
    END IF;
    IF audience='COMMUNITY' THEN
        RETURN EXISTS(SELECT 1 FROM communities c
            JOIN community_memberships own ON own.community_id=c.id AND own.user_account_id=target AND own.status='active'
            JOIN community_memberships other ON other.community_id=c.id AND other.user_account_id=viewer AND other.status='active'
            JOIN accounts a ON a.id=viewer AND a.account_type='person' AND a.status='active'
            WHERE c.id::text IN (SELECT value#>>'{}' FROM jsonb_array_elements(field_rule->'communityIds'))
              AND c.lifecycle_status='active' AND c.publication_status='published'
              AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()));
    END IF;
    RETURN false; -- PRIVATE / AGENT_ONLY / unknown never authorize another human or model.
END $$;

COMMIT;
