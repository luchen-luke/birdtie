BEGIN;

-- Native Person-owned manual declarations only. No backfill, Profile version
-- advance, grant, Runtime reader, candidate producer or inferred writer is
-- installed. EXPLICIT confidence=1 means directly declared, not probability.
CREATE FUNCTION birdtie_agent_memory_text_valid(value text, is_key boolean)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT strpos(value,chr(65533))=0
      AND (NOT is_key OR (value<>'' AND octet_length(value)<=100))
      AND NOT EXISTS (
        SELECT 1 FROM regexp_split_to_table(value,'') part
        WHERE part<>'' AND ((ascii(part) BETWEEN 0 AND 31 AND (is_key OR ascii(part) NOT IN (9,10)))
           OR ascii(part) BETWEEN 127 AND 159)
      );
$$;

-- jsonb has already erased duplicate raw keys. Wire/domain decoders must reject
-- them before conversion; this helper never claims to recover that information.
-- A compact encoding approximates Go's semantic byte bound, including the
-- default Go HTML/U+2028/U+2029 escapes. PostgreSQL normalizes number spelling;
-- both the raw-jsonb 16KiB and compact 8KiB limits remain explicit guards.
CREATE FUNCTION birdtie_agent_memory_compact_json(value jsonb, depth integer DEFAULT 1)
RETURNS text LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
    kind text:=jsonb_typeof(value);
    encoded text;
    item record;
    child text;
    separator text:='';
BEGIN
    IF depth NOT BETWEEN 1 AND 6 THEN RETURN NULL; END IF;
    IF kind='object' THEN
        IF (SELECT count(*) FROM jsonb_object_keys(value))>32 THEN RETURN NULL; END IF;
        encoded:='{';
        FOR item IN SELECT key,val FROM jsonb_each(value) AS entries(key,val) ORDER BY key COLLATE "C" LOOP
            child:=birdtie_agent_memory_compact_json(to_jsonb(item.key),depth+1);
            IF child IS NULL THEN RETURN NULL; END IF;
            encoded:=encoded||separator||child||':';
            child:=birdtie_agent_memory_compact_json(item.val,depth+1);
            IF child IS NULL THEN RETURN NULL; END IF;
            encoded:=encoded||child;
            separator:=',';
        END LOOP;
        RETURN encoded||'}';
    ELSIF kind='array' THEN
        IF jsonb_array_length(value)>32 THEN RETURN NULL; END IF;
        encoded:='[';
        FOR item IN SELECT val FROM jsonb_array_elements(value) AS entries(val) LOOP
            child:=birdtie_agent_memory_compact_json(item.val,depth+1);
            IF child IS NULL THEN RETURN NULL; END IF;
            encoded:=encoded||separator||child;
            separator:=',';
        END LOOP;
        RETURN encoded||']';
    ELSIF kind='string' THEN
        encoded:=value::text;
        encoded:=replace(replace(replace(encoded,'&','\u0026'),'<','\u003c'),'>','\u003e');
        RETURN replace(replace(encoded,chr(8232),'\u2028'),chr(8233),'\u2029');
    ELSE
        RETURN value::text;
    END IF;
END $$;

CREATE FUNCTION birdtie_agent_memory_value_valid(value jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
    item record;
    kind text;
    count_nodes integer:=0;
    compact text;
    numeric_value double precision;
BEGIN
    IF jsonb_typeof(value)<>'object' OR octet_length(value::text)>16384 THEN RETURN false; END IF;
    FOR item IN
      WITH RECURSIVE nodes(node,depth) AS (
        SELECT value,1
        UNION ALL
        SELECT child.node,parent.depth+1
        FROM nodes parent CROSS JOIN LATERAL (
          SELECT val AS node FROM jsonb_each(CASE WHEN jsonb_typeof(parent.node)='object'
              THEN parent.node ELSE '{}'::jsonb END) AS entries(key,val)
          UNION ALL
          SELECT val FROM jsonb_array_elements(CASE WHEN jsonb_typeof(parent.node)='array'
              THEN parent.node ELSE '[]'::jsonb END) AS entries(val)
        ) child
        WHERE parent.depth<=6
      ) SELECT node,depth FROM nodes LIMIT 257
    LOOP
        count_nodes:=count_nodes+1;
        IF count_nodes>256 OR item.depth>6 THEN RETURN false; END IF;
        kind:=jsonb_typeof(item.node);
        IF kind='object' THEN
            IF (SELECT count(*) FROM jsonb_object_keys(item.node))>32 OR EXISTS(
                SELECT 1 FROM jsonb_object_keys(item.node) key
                WHERE NOT birdtie_agent_memory_text_valid(key,true)) THEN RETURN false; END IF;
        ELSIF kind='array' THEN
            IF jsonb_array_length(item.node)>32 THEN RETURN false; END IF;
        ELSIF kind='string' THEN
            IF NOT birdtie_agent_memory_text_valid(item.node#>>'{}',false) THEN RETURN false; END IF;
        ELSIF kind='number' THEN
            IF octet_length(item.node::text)>64 THEN RETURN false; END IF;
            BEGIN
                numeric_value:=(item.node::text)::double precision;
            EXCEPTION WHEN numeric_value_out_of_range THEN RETURN false;
            END;
            IF numeric_value NOT BETWEEN '-1.7976931348623157e308'::double precision
                AND '1.7976931348623157e308'::double precision THEN RETURN false; END IF;
        ELSIF kind NOT IN ('boolean','null') THEN RETURN false;
        END IF;
    END LOOP;
    compact:=birdtie_agent_memory_compact_json(value);
    RETURN compact IS NOT NULL AND octet_length(compact)<=8192;
END $$;

CREATE TABLE agent_memories (
    id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'::uuid),
    schema_version text NOT NULL DEFAULT 'agent-memory-v1' CHECK(schema_version='agent-memory-v1'),
    agent_id uuid NOT NULL CHECK(agent_id<>'00000000-0000-0000-0000-000000000000'::uuid),
    owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
    owner_id uuid NOT NULL CHECK(owner_id<>'00000000-0000-0000-0000-000000000000'::uuid),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    memory_type text NOT NULL CHECK(memory_type IN ('IDENTITY','PREFERENCE','PLACE','CITY','ACTIVITY',
        'COMMUNITY','ORGANIZATION','RELATIONSHIP_CONTEXT','HISTORY','INTENT','ROUTINE','AVAILABILITY','EXPERIENCE')),
    memory_key text NOT NULL CHECK(memory_key ~ '^[a-z0-9][a-z0-9._:-]{0,99}$'),
    summary text NOT NULL,
    structured_value jsonb NOT NULL CHECK(birdtie_agent_memory_value_valid(structured_value)),
    confidence double precision NOT NULL DEFAULT 1 CHECK(confidence>=0 AND confidence<=1),
    source_type text NOT NULL DEFAULT 'EXPLICIT' CHECK(source_type IN ('EXPLICIT','INFERRED')),
    visibility text NOT NULL DEFAULT 'PRIVATE' CHECK(visibility IN ('PRIVATE','AGENT_ONLY')),
    status text NOT NULL DEFAULT 'ACTIVE',
    valid_from timestamptz NOT NULL DEFAULT clock_timestamp(),
    valid_until timestamptz NOT NULL,
    last_reinforced_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT agent_memory_person_binding FOREIGN KEY(agent_id,owner_id,owner_type)
        REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
    CONSTRAINT agent_memory_source_lifecycle CHECK (
        (source_type='EXPLICIT' AND confidence=1 AND last_reinforced_at IS NULL
            AND status IN ('ACTIVE','EXPIRED','DELETED')) OR
        (source_type='INFERRED' AND status IN ('PENDING_REVIEW','EXPIRED','DELETED'))
    ),
    CONSTRAINT agent_memory_tombstone CHECK (
        (status='DELETED' AND summary='' AND structured_value='{}'::jsonb AND last_reinforced_at IS NULL) OR
        (status<>'DELETED' AND octet_length(summary) BETWEEN 1 AND 1200
            AND birdtie_agent_memory_text_valid(summary,false)
            AND summary=btrim(summary,chr(9)||chr(10)||chr(11)||chr(12)||chr(13)||chr(32)||chr(133)||chr(160)||
                chr(5760)||chr(8192)||chr(8193)||chr(8194)||chr(8195)||chr(8196)||chr(8197)||chr(8198)||
                chr(8199)||chr(8200)||chr(8201)||chr(8202)||chr(8232)||chr(8233)||chr(8239)||chr(8287)||chr(12288)))
    ),
    CONSTRAINT agent_memory_times CHECK (
        isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at
        AND created_at>='0001-01-01 00:00:00+00'::timestamptz
        AND updated_at<'10000-01-01 00:00:00+00'::timestamptz
        AND isfinite(valid_from) AND isfinite(valid_until) AND valid_until>valid_from
        AND valid_from>='0001-01-01 00:00:00+00'::timestamptz
        AND valid_until<'10000-01-01 00:00:00+00'::timestamptz
        AND valid_until-valid_from<=interval '365 days'
        AND (last_reinforced_at IS NULL OR (isfinite(last_reinforced_at)
            AND last_reinforced_at>=created_at AND last_reinforced_at<=updated_at))
    )
);

-- Expired/deleted rows release the semantic key for a new declaration ID.
-- The old deleted ID is nevertheless a terminal tombstone, not an upsert slot.
CREATE UNIQUE INDEX agent_memory_current_key ON agent_memories(agent_id,memory_type,memory_key,source_type)
    WHERE status IN ('ACTIVE','PENDING_REVIEW');
CREATE INDEX agent_memory_owner ON agent_memories(owner_type,owner_id,updated_at DESC,id);

CREATE FUNCTION birdtie_guard_agent_memory() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        -- No session/role/global waiver. Only actual parent removal may cascade
        -- past the durable tombstone requirement, as with other bound content.
        IF EXISTS(SELECT 1 FROM agent_profiles
            WHERE agent_id=OLD.agent_id AND owner_id=OLD.owner_id AND owner_type=OLD.owner_type FOR KEY SHARE) THEN
            RAISE EXCEPTION 'Memory deletion requires versioned tombstone';
        END IF;
        RETURN OLD;
    ELSIF TG_OP='INSERT' THEN
        IF NEW.version<>1 THEN RAISE EXCEPTION 'new Memory starts at version one'; END IF;
    ELSE
        IF NEW.id IS DISTINCT FROM OLD.id OR NEW.agent_id IS DISTINCT FROM OLD.agent_id OR
           NEW.owner_id IS DISTINCT FROM OLD.owner_id OR NEW.owner_type IS DISTINCT FROM OLD.owner_type OR
           NEW.source_type IS DISTINCT FROM OLD.source_type OR NEW.created_at IS DISTINCT FROM OLD.created_at OR
           NEW.schema_version IS DISTINCT FROM OLD.schema_version THEN
            RAISE EXCEPTION 'Memory binding and source nature are immutable';
        END IF;
        IF OLD.status='DELETED' THEN RAISE EXCEPTION 'deleted Memory cannot be changed or restored'; END IF;
        IF OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'Memory revision must advance exactly once';
        END IF;
        NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER agent_memory_guard BEFORE INSERT OR UPDATE OR DELETE ON agent_memories
    FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_memory();

COMMIT;
