BEGIN;
-- Original membership and root identity remain append-only. Display order is
-- a separate current owner-approved projection and retired rows remain facts.
ALTER TABLE moment_thread_members DROP CONSTRAINT moment_thread_members_position_check;
ALTER TABLE moment_thread_members ADD CONSTRAINT moment_thread_members_position_check CHECK(position>=0);
CREATE TABLE moment_thread_edit_states(
 thread_id uuid PRIMARY KEY REFERENCES moment_threads(id),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 8999999999999999),
 operation_id uuid NOT NULL
);
CREATE TABLE moment_thread_display_members(
 thread_id uuid NOT NULL REFERENCES moment_thread_edit_states(thread_id),
 moment_id uuid NOT NULL REFERENCES moment_thread_members(moment_id),
 display_position integer CHECK(display_position BETWEEN 0 AND 19),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 8999999999999999),
 PRIMARY KEY(thread_id,moment_id),
 UNIQUE(thread_id,display_position) DEFERRABLE INITIALLY DEFERRED
);
CREATE FUNCTION birdtie_guard_moment_thread_edit_state() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'editing history is retained'; END IF;
 IF TG_OP='UPDATE' AND(NEW.thread_id<>OLD.thread_id OR NEW.version<>OLD.version+1 OR NEW.operation_id=OLD.operation_id) THEN RAISE EXCEPTION 'exact editing generation required';END IF;
 RETURN NEW;
END$$;
CREATE TRIGGER moment_thread_edit_state_guard BEFORE INSERT OR UPDATE OR DELETE ON moment_thread_edit_states FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_thread_edit_state();
CREATE FUNCTION birdtie_guard_moment_thread_display() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'retired display membership is retained';END IF;
 IF TG_OP='UPDATE' AND(NEW.thread_id<>OLD.thread_id OR NEW.moment_id<>OLD.moment_id OR NEW.version<>OLD.version+1) THEN RAISE EXCEPTION 'stable display membership and generation required';END IF;
 IF NOT EXISTS(SELECT 1 FROM moment_thread_members x JOIN moment_thread_edit_states e ON e.thread_id=x.thread_id WHERE x.thread_id=NEW.thread_id AND x.moment_id=NEW.moment_id AND e.version=NEW.version) THEN RAISE EXCEPTION 'matching original membership required';END IF;
 RETURN NEW;
END$$;
CREATE TRIGGER moment_thread_display_guard BEFORE INSERT OR UPDATE OR DELETE ON moment_thread_display_members FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_thread_display();
CREATE FUNCTION birdtie_check_moment_thread_edit() RETURNS trigger LANGUAGE plpgsql AS $$DECLARE tid uuid; active_count int; BEGIN
 tid:=NEW.thread_id;
 SELECT count(*) INTO active_count FROM moment_thread_display_members WHERE thread_id=tid AND display_position IS NOT NULL;
 IF active_count NOT BETWEEN 1 AND 20 OR (SELECT max(display_position) FROM moment_thread_display_members WHERE thread_id=tid)<>active_count-1
 OR EXISTS(SELECT 1 FROM moment_thread_members x WHERE x.thread_id=tid AND NOT EXISTS(SELECT 1 FROM moment_thread_display_members d WHERE d.thread_id=tid AND d.moment_id=x.moment_id))
 OR EXISTS(SELECT 1 FROM moment_thread_display_members d JOIN moment_thread_edit_states e ON e.thread_id=d.thread_id JOIN moments m ON m.id=d.moment_id WHERE d.thread_id=tid AND(d.version<>e.version OR(d.display_position IS NULL AND m.status<>'withdrawn')))
 OR NOT EXISTS(SELECT 1 FROM moment_thread_edit_states e JOIN moment_threads t ON t.id=e.thread_id JOIN moment_thread_operations op ON op.actor_account_id=t.author_account_id AND op.operation_id=e.operation_id AND op.thread_id=t.id AND op.status='SUCCEEDED' WHERE e.thread_id=tid AND e.version=t.version AND op.receipt->>'schemaVersion'='moment-thread-operation-v2' AND (op.receipt->>'appliedVersion')::bigint=e.version)
 THEN RAISE EXCEPTION 'complete approved current display required';END IF;
 RETURN NULL;
END$$;
CREATE CONSTRAINT TRIGGER moment_thread_edit_shape AFTER INSERT OR UPDATE ON moment_thread_edit_states DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_moment_thread_edit();
CREATE CONSTRAINT TRIGGER moment_thread_display_shape AFTER INSERT OR UPDATE ON moment_thread_display_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_moment_thread_edit();

CREATE OR REPLACE FUNCTION birdtie_check_moment_thread_shape() RETURNS trigger LANGUAGE plpgsql AS $$DECLARE tid uuid;total int;last_position int;minimum int;BEGIN
 IF TG_TABLE_NAME='moment_threads' THEN tid:=NEW.id;ELSE tid:=NEW.thread_id;END IF;
 IF EXISTS(SELECT 1 FROM moment_thread_edit_states WHERE thread_id=tid) THEN
 SELECT count(*),max(position) INTO total,last_position FROM moment_thread_members WHERE thread_id=tid;
 IF total<1 OR last_position<>total-1 OR NOT EXISTS(SELECT 1 FROM moment_threads t JOIN moment_thread_members x ON x.thread_id=t.id AND x.position=0 AND x.moment_id=t.root_moment_id WHERE t.id=tid) THEN RAISE EXCEPTION 'stable contiguous historical membership required';END IF;
 RETURN NULL;END IF;
 SELECT CASE WHEN audience='PRIVATE' THEN 1 ELSE 2 END INTO minimum FROM moment_threads WHERE id=tid;
 SELECT count(*),max(position) INTO total,last_position FROM moment_thread_members WHERE thread_id=tid;
 IF total<minimum OR total>20 OR last_position<>total-1 OR NOT EXISTS(SELECT 1 FROM moment_threads t JOIN moment_thread_members m ON m.thread_id=t.id AND m.position=0 AND m.moment_id=t.root_moment_id WHERE t.id=tid) THEN RAISE EXCEPTION 'complete ordered thread required';END IF;RETURN NULL;END$$;
CREATE OR REPLACE FUNCTION birdtie_guard_moment_thread() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'thread identity is retained';END IF;
 IF TG_OP='INSERT' AND NEW.audience='PRIVATE' THEN
 IF NEW.status<>'draft' OR NEW.version<>1 OR NOT EXISTS(SELECT 1 FROM legacy_native_context_entities x JOIN accounts a ON a.id=x.owner_account_id AND a.account_type='person' AND a.status IN('active','suspended') WHERE x.kind='thread' AND x.target_id=NEW.id AND x.owner_account_id=NEW.author_account_id)THEN RAISE EXCEPTION 'private original import provenance required';END IF;RETURN NEW;END IF;
 IF TG_OP='UPDATE' THEN
 IF NEW.id<>OLD.id OR NEW.author_account_id<>OLD.author_account_id OR NEW.root_moment_id<>OLD.root_moment_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'stable original thread required';END IF;
 IF EXISTS(SELECT 1 FROM moment_thread_edit_states e WHERE e.thread_id=OLD.id AND e.version=NEW.version) THEN
 IF NEW.audience<>'PUBLIC' OR NEW.status NOT IN('published','withdrawn') THEN RAISE EXCEPTION 'explicit public editing state required';END IF;
 IF NEW.status='withdrawn' AND EXISTS(SELECT 1 FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id WHERE x.thread_id=OLD.id AND m.status<>'withdrawn') THEN RAISE EXCEPTION 'all children must withdraw';END IF;
 ELSIF OLD.audience='PRIVATE'  AND OLD.status='draft' AND NEW.audience='PUBLIC' AND NEW.status='published' THEN
 IF(SELECT count(*)FROM moment_thread_members WHERE thread_id=OLD.id)NOT BETWEEN 2 AND 20 OR EXISTS(SELECT 1 FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id WHERE x.thread_id=OLD.id AND(m.status<>'published' OR m.visibility<>'public' OR m.author_account_id<>OLD.author_account_id))THEN RAISE EXCEPTION 'all original children must publish atomically';END IF;
 ELSIF OLD.audience<>'PUBLIC' OR OLD.status<>'published' OR NEW.audience<>'PUBLIC' OR NEW.status<>'withdrawn' OR EXISTS(SELECT 1 FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id WHERE x.thread_id=OLD.id AND m.status<>'withdrawn')THEN RAISE EXCEPTION 'exact whole-thread transition required';END IF;END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.author_account_id AND account_type='person' AND status='active')THEN RAISE EXCEPTION 'active Person required';END IF;RETURN NEW;END$$;
CREATE OR REPLACE FUNCTION birdtie_guard_moment_thread_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'original thread operation is immutable';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text || ':MOMENT_THREAD:' || NEW.operation_id::text, 153));
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id = NEW.actor_account_id
                   AND account_type = 'person' AND status = 'active') THEN
        RAISE EXCEPTION 'active Person required';
    END IF;
    IF NEW.receipt->>'schemaVersion'='moment-thread-operation-v2' THEN
      IF NEW.status<>'SUCCEEDED' OR NEW.receipt->>'operationId'<>NEW.operation_id::text OR NEW.receipt->>'status'<>'SUCCEEDED'
      OR NEW.receipt->>'action' NOT IN('EDIT','WITHDRAW') OR jsonb_typeof(NEW.receipt->'members') IS DISTINCT FROM 'array' OR jsonb_typeof(NEW.receipt->'removedMembers') IS DISTINCT FROM 'array'
      OR jsonb_array_length(NEW.receipt->'removedMembers')>20
      OR jsonb_array_length(NEW.receipt->'removedMembers')<>(SELECT count(DISTINCT r->>'momentId') FROM jsonb_array_elements(NEW.receipt->'removedMembers') r)
      OR NOT EXISTS(SELECT 1 FROM moment_threads t JOIN moment_thread_edit_states e ON e.thread_id=t.id WHERE t.id=NEW.thread_id AND t.author_account_id=NEW.actor_account_id AND e.operation_id=NEW.operation_id AND e.version=t.version
        AND NEW.receipt->>'threadId'=t.id::text AND NEW.receipt->>'rootMomentId'=t.root_moment_id::text AND NEW.receipt->>'audience'='PUBLIC'
        AND (NEW.receipt->>'appliedVersion')::bigint=t.version AND (NEW.receipt->>'expectedVersion')::bigint=t.version-1 AND NEW.receipt->>'payloadDigest'=encode(NEW.payload_sha256,'hex')
        AND ((NEW.receipt->>'action'='EDIT' AND t.status='published')OR(NEW.receipt->>'action'='WITHDRAW' AND t.status='withdrawn'))
        AND jsonb_array_length(NEW.receipt->'members')=(SELECT count(*) FROM moment_thread_display_members d WHERE d.thread_id=t.id AND d.display_position IS NOT NULL)
        AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(NEW.receipt->'members') WITH ORDINALITY q(r,n) WHERE (r->>'position')::integer<>n-1 OR NOT EXISTS(SELECT 1 FROM moment_thread_display_members d JOIN moments m ON m.id=d.moment_id WHERE d.thread_id=t.id AND d.display_position=(r->>'position')::integer AND m.id::text=r->>'momentId' AND m.revision=(r->>'appliedRevision')::bigint AND m.status=r->>'status' AND ((NEW.receipt->>'action'='EDIT' AND m.status='published' AND m.visibility='public')OR(NEW.receipt->>'action'='WITHDRAW' AND m.status='withdrawn'))))
        AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(NEW.receipt->'removedMembers') r WHERE NOT EXISTS(SELECT 1 FROM moment_thread_display_members d JOIN moment_thread_members x ON x.thread_id=d.thread_id AND x.moment_id=d.moment_id JOIN moments m ON m.id=d.moment_id WHERE d.thread_id=t.id AND d.display_position IS NULL AND x.position=(r->>'position')::integer AND m.id::text=r->>'momentId' AND m.revision=(r->>'appliedRevision')::bigint AND m.status='withdrawn' AND r->>'status'='withdrawn'))
      ) THEN RAISE EXCEPTION 'matching approved edited thread effect required';END IF;
      RETURN NEW;
    END IF;
    IF NEW.receipt->>'schemaVersion'  <> 'moment-thread-operation-v1'
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
            OR (NEW.receipt->>'action' = 'PUBLISH' AND t.status='published' AND t.audience='PUBLIC' AND t.version>1)
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
CREATE FUNCTION birdtie_moment_thread_editing_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'moment-thread-editing-v1'::text$$;
COMMIT;
