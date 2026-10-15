BEGIN;
ALTER TABLE places ALTER COLUMN city_id DROP NOT NULL;
ALTER TABLE places ADD CONSTRAINT place_unknown_city_private CHECK(city_id IS NOT NULL OR publication_status IN('draft','hidden'));
CREATE TABLE legacy_native_context_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_native_adaptation_runs(run_id),protocol text NOT NULL CHECK(protocol='birdtie-legacy-native-context-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL,deferred jsonb NOT NULL,committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),CHECK(jsonb_typeof(counts)='object' AND jsonb_typeof(deferred)='object'));
CREATE TABLE legacy_native_context_entities(
 run_id uuid NOT NULL REFERENCES legacy_native_context_runs(run_id) DEFERRABLE INITIALLY DEFERRED,kind text NOT NULL CHECK(kind IN('unplaced_place','saved_place','note_location','media_context','media_place','thread','thread_member','thread_leg')),
 source_key text NOT NULL,source_sha256 text NOT NULL CHECK(source_sha256~'^[0-9a-f]{64}$'),target_id uuid NOT NULL,owner_account_id uuid REFERENCES accounts(id),source_parent_key text,source_parent_sha256 text,source_region text,source_country_code text,source_area_kind text,
 PRIMARY KEY(kind,source_key),UNIQUE(kind,target_id),CHECK((kind='unplaced_place')=(owner_account_id IS NULL)));
CREATE TABLE private_moment_media_contexts(
 id uuid PRIMARY KEY,moment_id uuid NOT NULL REFERENCES moments(id),owner_account_id uuid NOT NULL REFERENCES accounts(id),
 asset_kind text NOT NULL CHECK(asset_kind IN('IMAGE','VIDEO')),image_id uuid REFERENCES private_moment_images(id),video_id uuid REFERENCES private_moment_videos(id),source_position integer NOT NULL CHECK(source_position>=0),
 captured_date text CHECK(captured_date IS NULL OR captured_date~'^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),original_map_enabled boolean NOT NULL,
 source_latitude numeric,source_longitude numeric,source_coordinate_system text CHECK(source_coordinate_system IS NULL),map_enabled boolean NOT NULL DEFAULT false CHECK(NOT map_enabled),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),status text NOT NULL DEFAULT 'ACTIVE' CHECK(status IN('ACTIVE','REMOVED')),
 CHECK((asset_kind='IMAGE' AND image_id IS NOT NULL AND video_id IS NULL)OR(asset_kind='VIDEO' AND image_id IS NULL AND video_id IS NOT NULL)),
 CHECK(source_latitude IS NULL OR source_latitude BETWEEN -90 AND 90),CHECK(source_longitude IS NULL OR source_longitude BETWEEN -180 AND 180));
CREATE TABLE private_moment_media_context_places(id uuid PRIMARY KEY,context_id uuid NOT NULL REFERENCES private_moment_media_contexts(id),position integer NOT NULL CHECK(position>=0),place_id uuid NOT NULL REFERENCES places(id),UNIQUE(context_id,position));
CREATE TABLE moment_thread_entry_contexts(thread_id uuid NOT NULL REFERENCES moment_threads(id),moment_id uuid NOT NULL REFERENCES moments(id),position integer NOT NULL,
 inbound_origin text NOT NULL,inbound_mode text NOT NULL,duration_minutes bigint,estimated_duration_minutes bigint,PRIMARY KEY(thread_id,moment_id),UNIQUE(thread_id,position),CHECK(duration_minutes IS NULL OR duration_minutes>=0),CHECK(estimated_duration_minutes IS NULL OR estimated_duration_minutes>=0));
CREATE TABLE moment_thread_route_legs(id uuid PRIMARY KEY,thread_id uuid NOT NULL REFERENCES moment_threads(id),from_moment_id uuid NOT NULL REFERENCES moments(id),to_moment_id uuid NOT NULL REFERENCES moments(id),
 from_position integer NOT NULL,to_position integer NOT NULL,mode text NOT NULL,from_place_id uuid REFERENCES places(id),to_place_id uuid REFERENCES places(id),source_created_at timestamptz,source_updated_at timestamptz,UNIQUE(thread_id,from_position,to_position));
CREATE INDEX owned_media_contexts ON private_moment_media_contexts(owner_account_id,moment_id,source_position,id);
ALTER TABLE moment_threads DROP CONSTRAINT moment_threads_audience_check;
ALTER TABLE moment_threads DROP CONSTRAINT moment_threads_status_check;
ALTER TABLE moment_threads ADD CONSTRAINT moment_threads_audience_check CHECK(audience IN('PUBLIC','PRIVATE'));
ALTER TABLE moment_threads ADD CONSTRAINT moment_threads_status_check CHECK((audience='PRIVATE' AND status='draft')OR(audience='PUBLIC' AND status IN('published','withdrawn')));
CREATE OR REPLACE FUNCTION birdtie_guard_moment_thread_member() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original thread membership is immutable';END IF;
 IF NOT EXISTS(SELECT 1 FROM moment_threads t JOIN moments m ON m.id=NEW.moment_id AND m.author_account_id=t.author_account_id
 WHERE t.id=NEW.thread_id AND((t.audience='PUBLIC' AND t.status='published' AND m.status='published' AND m.visibility='public')
 OR(t.audience='PRIVATE' AND t.status='draft' AND m.status='draft' AND m.visibility='private' AND EXISTS(SELECT 1 FROM legacy_native_context_entities x JOIN legacy_native_adaptation_entities n ON n.kind='moment' AND n.target_id=m.id::text AND n.owner_account_id=t.author_account_id AND n.run_id=x.run_id WHERE x.kind='thread' AND x.target_id=t.id AND x.owner_account_id=t.author_account_id))))THEN RAISE EXCEPTION 'matching current child and private import provenance required';END IF;RETURN NEW;END$$;
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
-- Retain the original153 operation guard byte-for-byte except its explicit new
-- existing-thread PUBLISH transition; it still validates every original child.
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

CREATE FUNCTION birdtie_check_legacy_context_entity() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_native_context_runs r JOIN legacy_import_runs o ON o.run_id=r.run_id AND o.source_file_sha256=r.source_file_sha256 AND o.manifest_file_sha256=r.manifest_file_sha256 WHERE r.run_id=NEW.run_id) THEN RAISE EXCEPTION 'matching preserved source required';END IF;
 IF NEW.owner_account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM legacy_entity_mappings u JOIN accounts a ON a.id=u.target_id WHERE u.run_id=NEW.run_id AND u.entity_kind='user' AND u.owner_account_id=NEW.owner_account_id AND u.target_id=NEW.owner_account_id AND a.account_type='person' AND a.status IN('active','suspended'))THEN RAISE EXCEPTION 'exact original owner required';END IF;
 IF NEW.kind='unplaced_place' AND NOT EXISTS(SELECT 1 FROM places p WHERE p.id=NEW.target_id AND p.city_id IS NULL AND p.publication_status='draft' AND p.latitude IS NULL AND p.longitude IS NULL)THEN RAISE EXCEPTION 'private unplaced native draft required';END IF;
 IF NEW.kind='saved_place' AND NOT EXISTS(SELECT 1 FROM saved_items s JOIN legacy_personal_archives ar ON ar.kind='saved_place' AND ar.source_key=NEW.source_key AND ar.source_sha256=NEW.source_sha256 AND ar.run_id=NEW.run_id AND ar.owner_account_id=s.owner_account_id AND NOT ar.deleted WHERE s.id=NEW.target_id AND s.owner_account_id=NEW.owner_account_id)THEN RAISE EXCEPTION 'original saved source binding required';END IF;
 IF NEW.kind IN('note_location','media_context') AND NOT EXISTS(SELECT 1 FROM legacy_note_archives n JOIN legacy_native_adaptation_entities m ON m.run_id=n.run_id AND m.kind='moment' AND m.source_key=n.source_note_id::text AND m.source_sha256=n.source_sha256 AND m.owner_account_id=n.owner_account_id WHERE n.run_id=NEW.run_id AND n.source_note_id::text=NEW.source_parent_key AND n.source_sha256=NEW.source_parent_sha256 AND n.owner_account_id=NEW.owner_account_id AND NOT n.deleted)THEN RAISE EXCEPTION 'exact preserved live Note required';END IF;
 IF NEW.kind='thread' AND NOT EXISTS(SELECT 1 FROM moment_threads t WHERE t.id=NEW.target_id AND t.author_account_id=NEW.owner_account_id AND t.audience='PRIVATE' AND t.status='draft')THEN RAISE EXCEPTION 'original private thread required';END IF;
 RETURN NULL;END$$;
CREATE CONSTRAINT TRIGGER legacy_context_source_binding AFTER INSERT ON legacy_native_context_entities DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_legacy_context_entity();
CREATE TRIGGER legacy_context_runs_immutable BEFORE UPDATE OR DELETE ON legacy_native_context_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_context_entities_immutable BEFORE UPDATE OR DELETE ON legacy_native_context_entities FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER original_thread_entry_context_immutable BEFORE UPDATE OR DELETE ON moment_thread_entry_contexts FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER original_thread_route_leg_immutable BEFORE UPDATE OR DELETE ON moment_thread_route_legs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_context_counts()RETURNS trigger LANGUAGE plpgsql AS $$DECLARE actual jsonb;BEGIN
 SELECT jsonb_build_object('places',count(*)FILTER(WHERE kind='unplaced_place'),'savedItems',count(*)FILTER(WHERE kind='saved_place'),'noteLocations',count(*)FILTER(WHERE kind='note_location'),'mediaContexts',count(*)FILTER(WHERE kind='media_context'),'mediaPlaces',count(*)FILTER(WHERE kind='media_place'),'threads',count(*)FILTER(WHERE kind='thread'),'members',count(*)FILTER(WHERE kind='thread_member'),'legs',count(*)FILTER(WHERE kind='thread_leg'),'threadTombstones',NEW.counts->'threadTombstones')INTO actual FROM legacy_native_context_entities WHERE run_id=NEW.run_id;
 IF actual<>NEW.counts THEN RAISE EXCEPTION 'immutable context counts must match original effects';END IF;RETURN NULL;END$$;
CREATE CONSTRAINT TRIGGER legacy_context_counts AFTER INSERT ON legacy_native_context_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_context_counts();
CREATE FUNCTION birdtie_guard_private_media_context()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' OR TG_OP='UPDATE' AND(OLD.status='REMOVED' OR NEW.status<>'REMOVED' OR NEW.revision<>OLD.revision+1 OR(to_jsonb(NEW)-ARRAY['status','revision']) IS DISTINCT FROM(to_jsonb(OLD)-ARRAY['status','revision']))THEN RAISE EXCEPTION 'original media context immutable except retirement';END IF;RETURN NEW;END$$;
CREATE TRIGGER private_media_context_identity BEFORE UPDATE OR DELETE ON private_moment_media_contexts FOR EACH ROW EXECUTE FUNCTION birdtie_guard_private_media_context();
CREATE TRIGGER private_media_context_places_immutable BEFORE UPDATE OR DELETE ON private_moment_media_context_places FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_context_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'legacy-native-context-v1'::text$$;
COMMIT;
