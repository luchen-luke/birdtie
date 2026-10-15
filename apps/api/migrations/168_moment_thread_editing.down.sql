BEGIN;
-- Once an EDIT fact exists, old schema cannot represent its retained history.
DO $$BEGIN IF EXISTS(SELECT 1 FROM moment_thread_edit_states) THEN RAISE EXCEPTION 'editing facts require forward migration; rollback refused';END IF;END$$;
DROP FUNCTION birdtie_moment_thread_editing_contract();
DROP TABLE moment_thread_display_members;
DROP TABLE moment_thread_edit_states;
DROP FUNCTION birdtie_guard_moment_thread_display();
DROP FUNCTION birdtie_guard_moment_thread_edit_state();
DROP FUNCTION birdtie_check_moment_thread_edit();
ALTER TABLE moment_thread_members DROP CONSTRAINT moment_thread_members_position_check;
ALTER TABLE moment_thread_members ADD CONSTRAINT moment_thread_members_position_check CHECK(position BETWEEN 0 AND 19);
CREATE OR REPLACE FUNCTION birdtie_check_moment_thread_shape() RETURNS trigger LANGUAGE plpgsql AS $$DECLARE tid uuid;total int;last_position int;minimum int;BEGIN
 IF TG_TABLE_NAME='moment_threads' THEN tid:=NEW.id;ELSE tid:=NEW.thread_id;END IF;
 SELECT CASE WHEN audience='PRIVATE' THEN 1 ELSE 2 END INTO minimum FROM moment_threads WHERE id=tid;
 SELECT count(*),max(position) INTO total,last_position FROM moment_thread_members WHERE thread_id=tid;
 IF total<minimum OR total>20 OR last_position<>total-1 OR NOT EXISTS(SELECT 1 FROM moment_threads t JOIN moment_thread_members m ON m.thread_id=t.id AND m.position=0 AND m.moment_id=t.root_moment_id WHERE t.id=tid) THEN RAISE EXCEPTION 'complete ordered thread required';END IF;RETURN NULL;END$$;
CREATE OR REPLACE FUNCTION birdtie_guard_moment_thread() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'thread identity is retained';END IF;
 IF TG_OP='INSERT' AND NEW.audience='PRIVATE' THEN
 IF NEW.status<>'draft' OR NEW.version<>1 OR NOT EXISTS(SELECT 1 FROM legacy_native_context_entities x JOIN accounts a ON a.id=x.owner_account_id AND a.account_type='person' AND a.status IN('active','suspended') WHERE x.kind='thread' AND x.target_id=NEW.id AND x.owner_account_id=NEW.author_account_id)THEN RAISE EXCEPTION 'private original import provenance required';END IF;RETURN NEW;END IF;
 IF TG_OP='UPDATE' THEN
 IF NEW.id<>OLD.id OR NEW.author_account_id<>OLD.author_account_id OR NEW.root_moment_id<>OLD.root_moment_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'stable original thread required';END IF;
 IF OLD.audience='PRIVATE' AND OLD.status='draft' AND NEW.audience='PUBLIC' AND NEW.status='published' THEN
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
COMMIT;
