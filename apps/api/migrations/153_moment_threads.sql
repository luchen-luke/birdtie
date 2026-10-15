BEGIN;

CREATE TABLE moment_threads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_account_id uuid NOT NULL REFERENCES accounts(id),
    root_moment_id uuid NOT NULL UNIQUE REFERENCES moments(id),
    audience text NOT NULL CHECK (audience = 'PUBLIC'),
    version bigint NOT NULL DEFAULT 1 CHECK (version BETWEEN 1 AND 8999999999999999),
    status text NOT NULL DEFAULT 'published' CHECK (status IN ('published', 'withdrawn')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Membership is historical identity. Single-child edits and withdrawals never
-- renumber or remove it; all readers must authorize each current child.
CREATE TABLE moment_thread_members (
    thread_id uuid NOT NULL REFERENCES moment_threads(id),
    moment_id uuid NOT NULL UNIQUE REFERENCES moments(id),
    position integer NOT NULL CHECK (position BETWEEN 0 AND 19),
    PRIMARY KEY (thread_id, position)
);

CREATE TABLE moment_thread_operations (
    actor_account_id uuid NOT NULL REFERENCES accounts(id),
    operation_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('SUCCEEDED', 'STOPPED')),
    thread_id uuid REFERENCES moment_threads(id),
    payload_sha256 bytea,
    receipt jsonb NOT NULL,
    committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (actor_account_id, operation_id),
    CHECK ((status = 'STOPPED' AND thread_id IS NULL AND payload_sha256 IS NULL)
        OR (status = 'SUCCEEDED' AND thread_id IS NOT NULL AND octet_length(payload_sha256) = 32))
);

CREATE FUNCTION birdtie_guard_moment_thread_member() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'original thread membership is immutable';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM moment_threads t JOIN moments m ON m.id = NEW.moment_id
        WHERE t.id = NEW.thread_id AND t.author_account_id = m.author_account_id
          AND t.status = 'published' AND m.status = 'published' AND m.visibility = 'public'
    ) THEN
        RAISE EXCEPTION 'matching explicitly published child required';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER moment_thread_members_immutable BEFORE INSERT OR UPDATE OR DELETE
ON moment_thread_members FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_thread_member();

CREATE FUNCTION birdtie_check_moment_thread_shape() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    tid uuid;
    total integer;
    last_position integer;
BEGIN
    IF TG_TABLE_NAME = 'moment_threads' THEN
        tid := NEW.id;
    ELSE
        tid := NEW.thread_id;
    END IF;
    SELECT count(*), max(position) INTO total, last_position
      FROM moment_thread_members WHERE thread_id = tid;
    IF total < 2 OR total > 20 OR last_position <> total - 1 OR NOT EXISTS (
        SELECT 1 FROM moment_threads t JOIN moment_thread_members m
          ON m.thread_id = t.id AND m.position = 0 AND m.moment_id = t.root_moment_id
        WHERE t.id = tid
    ) THEN
        RAISE EXCEPTION 'complete ordered thread required';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER moment_thread_shape AFTER INSERT ON moment_thread_members
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_moment_thread_shape();
CREATE CONSTRAINT TRIGGER moment_thread_root_shape AFTER INSERT ON moment_threads
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_moment_thread_shape();

CREATE FUNCTION birdtie_guard_moment_thread() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'thread identity is retained';
    END IF;
    IF TG_OP = 'UPDATE' AND (
        NEW.id <> OLD.id OR NEW.author_account_id <> OLD.author_account_id
        OR NEW.root_moment_id <> OLD.root_moment_id OR NEW.audience <> OLD.audience
        OR NEW.created_at <> OLD.created_at OR OLD.status <> 'published'
        OR NEW.status <> 'withdrawn' OR NEW.version <> OLD.version + 1
        OR EXISTS (SELECT 1 FROM moment_thread_members x JOIN moments m ON m.id = x.moment_id
                   WHERE x.thread_id = OLD.id AND m.status <> 'withdrawn')
    ) THEN
        RAISE EXCEPTION 'exact whole-thread withdrawal required';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id = NEW.author_account_id
                   AND account_type = 'person' AND status = 'active') THEN
        RAISE EXCEPTION 'active Person required';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER moment_threads_guard BEFORE INSERT OR UPDATE OR DELETE ON moment_threads
FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_thread();

CREATE FUNCTION birdtie_guard_moment_thread_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'original thread operation is immutable';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text || ':MOMENT_THREAD:' || NEW.operation_id::text, 153));
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id = NEW.actor_account_id
                   AND account_type = 'person' AND status = 'active') THEN
        RAISE EXCEPTION 'active Person required';
    END IF;
    IF NEW.receipt->>'schemaVersion' <> 'moment-thread-operation-v1'
       OR NEW.receipt->>'operationId' <> NEW.operation_id::text
       OR NEW.receipt->>'status' <> NEW.status THEN
        RAISE EXCEPTION 'matching original receipt required';
    END IF;
    IF NEW.status = 'SUCCEEDED' AND NOT EXISTS (
        SELECT 1 FROM moment_threads t WHERE t.id = NEW.thread_id
          AND t.author_account_id = NEW.actor_account_id
          AND NEW.receipt->>'threadId' = t.id::text
          AND NEW.receipt->>'rootMomentId' = t.root_moment_id::text
          AND (NEW.receipt->>'appliedVersion')::bigint = t.version
          AND NEW.receipt->>'payloadDigest' = encode(NEW.payload_sha256, 'hex')
          AND ((NEW.receipt->>'action' = 'CREATE_PUBLISH' AND t.status = 'published' AND t.version = 1)
            OR (NEW.receipt->>'action' = 'WITHDRAW' AND t.status = 'withdrawn'))
          AND jsonb_array_length(NEW.receipt->'members') =
              (SELECT count(*) FROM moment_thread_members WHERE thread_id = t.id)
          AND NOT EXISTS (
              SELECT 1 FROM jsonb_array_elements(NEW.receipt->'members') r
              WHERE NOT EXISTS (SELECT 1 FROM moment_thread_members x JOIN moments m ON m.id = x.moment_id
                    WHERE x.thread_id = t.id AND x.position = (r->>'position')::integer
                      AND x.moment_id::text = r->>'momentId'
                      AND m.revision = (r->>'appliedRevision')::bigint
                      AND m.status = r->>'status')
          )
    ) THEN
        RAISE EXCEPTION 'matching complete canonical thread effect required';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER moment_thread_operations_immutable BEFORE INSERT OR UPDATE OR DELETE
ON moment_thread_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_thread_operation();

CREATE FUNCTION birdtie_moment_thread_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$
    SELECT 'moment-thread-v1'::text
$$;
COMMIT;
