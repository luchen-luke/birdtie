BEGIN;
-- Minimum original public-content attribution; never changes private profiles.
CREATE TABLE legacy_moment_author_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_map_visibility_runs(run_id),protocol text NOT NULL CHECK(protocol='birdtie-civu-moment-author-v1'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[0-9a-f]{64}$'),matcher_file_sha256 text NOT NULL CHECK(matcher_file_sha256~'^[0-9a-f]{64}$'),seal_sha256 text NOT NULL CHECK(seal_sha256~'^[0-9a-f]{64}$'),projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),counts jsonb NOT NULL CHECK(birdtie_legacy_nonnegative_counts(counts)),deferred jsonb NOT NULL CHECK(birdtie_legacy_nonnegative_counts(deferred)),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE legacy_moment_author_snapshots(
 run_id uuid NOT NULL REFERENCES legacy_moment_author_runs(run_id) DEFERRABLE INITIALLY DEFERRED,source_user_id uuid NOT NULL,owner_account_id uuid NOT NULL REFERENCES accounts(id),source_user_sha256 text NOT NULL CHECK(source_user_sha256~'^[0-9a-f]{64}$'),source_version bigint NOT NULL CHECK(source_version>0),display_name text NOT NULL CHECK(octet_length(display_name) BETWEEN 1 AND 16000),
 source_media_id uuid,source_media_sha256 text,input_sha256 text,input_bytes bigint,thumbnail bytea,thumbnail_sha256 text,
 PRIMARY KEY(run_id,source_user_id),UNIQUE(run_id,owner_account_id),
 CHECK((source_media_id IS NULL AND source_media_sha256 IS NULL AND input_sha256 IS NULL AND input_bytes IS NULL AND thumbnail IS NULL AND thumbnail_sha256 IS NULL)OR(source_media_id IS NOT NULL AND source_media_sha256 IS NOT NULL AND input_sha256 IS NOT NULL AND input_bytes IS NOT NULL AND thumbnail IS NOT NULL AND thumbnail_sha256 IS NOT NULL AND source_media_sha256~'^[0-9a-f]{64}$' AND input_sha256~'^[0-9a-f]{64}$' AND input_bytes BETWEEN 1 AND 16777216 AND octet_length(thumbnail) BETWEEN 1 AND 131072 AND thumbnail_sha256=encode(sha256(thumbnail),'hex'))));
CREATE TABLE legacy_moment_author_facts(
 moment_id uuid PRIMARY KEY REFERENCES moments(id),run_id uuid NOT NULL,source_user_id uuid NOT NULL,source_note_id uuid NOT NULL,source_note_sha256 text NOT NULL CHECK(source_note_sha256~'^[0-9a-f]{64}$'),source_created_at timestamptz,
 FOREIGN KEY(run_id,source_user_id)REFERENCES legacy_moment_author_snapshots(run_id,source_user_id) DEFERRABLE INITIALLY DEFERRED,UNIQUE(run_id,source_note_id));
CREATE FUNCTION birdtie_legacy_moment_author_guard()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_TABLE_NAME='legacy_moment_author_runs' THEN
 IF NOT EXISTS(SELECT 1 FROM legacy_import_runs o WHERE o.run_id=NEW.run_id AND o.source_file_sha256=NEW.source_file_sha256 AND o.manifest_file_sha256=NEW.manifest_file_sha256 AND o.snapshot_sha256=NEW.snapshot_sha256)
 OR(SELECT count(*)FROM legacy_moment_author_facts WHERE run_id=NEW.run_id)<>(NEW.counts->>'moments')::integer OR(SELECT count(*)FROM legacy_moment_author_snapshots WHERE run_id=NEW.run_id)<>(NEW.counts->>'authors')::integer OR(SELECT count(*)FROM legacy_moment_author_snapshots WHERE run_id=NEW.run_id AND source_media_id IS NOT NULL)<>(NEW.counts->>'avatars')::integer THEN RAISE EXCEPTION 'exact attribution source required';END IF;
 ELSIF TG_TABLE_NAME='legacy_moment_author_snapshots' THEN
 IF NOT EXISTS(SELECT 1 FROM legacy_entity_mappings u WHERE u.run_id=NEW.run_id AND u.entity_kind='user'AND u.source_id=NEW.source_user_id AND u.target_id=NEW.owner_account_id AND u.owner_account_id=NEW.owner_account_id AND u.source_sha256=NEW.source_user_sha256 AND u.source_version=NEW.source_version AND u.source_status='active')THEN RAISE EXCEPTION 'exact original author required';END IF;
 ELSE
 IF NOT EXISTS(SELECT 1 FROM legacy_native_adaptation_entities x JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key AND n.source_sha256=x.source_sha256 AND n.owner_account_id=x.owner_account_id JOIN legacy_moment_author_snapshots a ON a.run_id=x.run_id AND a.owner_account_id=x.owner_account_id WHERE x.kind='moment'AND x.target_id=NEW.moment_id::text AND x.run_id=NEW.run_id AND n.source_note_id=NEW.source_note_id AND n.source_sha256=NEW.source_note_sha256 AND a.source_user_id=NEW.source_user_id AND NOT n.deleted AND n.source_visibility='public'AND n.source_moderation_status='ok')THEN RAISE EXCEPTION 'original public content attribution required';END IF;
 END IF;RETURN NEW;END$$;
CREATE CONSTRAINT TRIGGER legacy_moment_author_run_binding AFTER INSERT ON legacy_moment_author_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_moment_author_guard();
CREATE CONSTRAINT TRIGGER legacy_moment_author_snapshot_binding AFTER INSERT ON legacy_moment_author_snapshots DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_moment_author_guard();
CREATE CONSTRAINT TRIGGER legacy_moment_author_fact_binding AFTER INSERT ON legacy_moment_author_facts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_moment_author_guard();
CREATE TRIGGER legacy_moment_author_runs_immutable BEFORE UPDATE OR DELETE ON legacy_moment_author_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_moment_author_snapshots_immutable BEFORE UPDATE OR DELETE ON legacy_moment_author_snapshots FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_moment_author_facts_immutable BEFORE UPDATE OR DELETE ON legacy_moment_author_facts FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_moment_author_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'legacy-moment-author-v1'::text$$;
COMMIT;
