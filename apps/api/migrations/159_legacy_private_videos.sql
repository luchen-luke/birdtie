BEGIN;
-- Native owned attachments, with an explicit authenticated import provenance.
--125's clean-scanner / real upload approval contract remains unchanged.
CREATE TABLE legacy_private_video_import_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_native_adaptation_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-private-videos-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 matcher_file_sha256 text NOT NULL CHECK(matcher_file_sha256~'^[0-9a-f]{64}$'),
 authenticated_seal_sha256 text NOT NULL CHECK(authenticated_seal_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE private_moment_videos(
 id uuid PRIMARY KEY REFERENCES media_assets(id),
 run_id uuid NOT NULL REFERENCES legacy_private_video_import_runs(run_id),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),moment_id uuid NOT NULL REFERENCES moments(id),
 source_position integer NOT NULL CHECK(source_position>=0),
 input_size bigint NOT NULL CHECK(input_size BETWEEN 1 AND 67108864),
 input_sha256 text NOT NULL CHECK(input_sha256~'^[0-9a-f]{64}$'),
 original bytea,content bytea,poster bytea,
 content_size bigint NOT NULL CHECK(content_size BETWEEN 1 AND 67108864),
 content_sha256 text NOT NULL CHECK(content_sha256~'^[0-9a-f]{64}$'),
 poster_size bigint NOT NULL CHECK(poster_size BETWEEN 1 AND 5242880),
 poster_sha256 text NOT NULL CHECK(poster_sha256~'^[0-9a-f]{64}$'),
 width integer NOT NULL CHECK(width BETWEEN 2 AND 1920),height integer NOT NULL CHECK(height BETWEEN 2 AND 1920),
 duration_ms bigint NOT NULL CHECK(duration_ms BETWEEN 1 AND 120000),frame_count bigint NOT NULL CHECK(frame_count BETWEEN 1 AND 7200),
 audio_codec text NOT NULL CHECK(audio_codec IN('aac','none')),
 audio_packet_count bigint NOT NULL CHECK(audio_packet_count BETWEEN 0 AND 10000),audio_packet_sha256 text NOT NULL,
 CHECK((audio_codec='none' AND audio_packet_count=0 AND audio_packet_sha256='') OR(audio_codec='aac' AND audio_packet_count>0 AND audio_packet_sha256~'^[0-9a-f]{64}$')),
 validation_kind text NOT NULL CHECK(validation_kind='FULL_VIDEO_DECODE_CANONICAL_REENCODE_V1'),
 antivirus_scan text NOT NULL CHECK(antivirus_scan='NOT_RUN'),privacy_risk text NOT NULL CHECK(privacy_risk='UNKNOWN'),
 status text NOT NULL CHECK(status IN('ready_private','deleted')),revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 retain_until timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(width::bigint*height<=2073600),CHECK(retain_until>created_at),
 CHECK((status='ready_private' AND original IS NOT NULL AND content IS NOT NULL AND poster IS NOT NULL
 AND octet_length(original)=input_size AND octet_length(content)=content_size AND octet_length(poster)=poster_size)
 OR(status='deleted' AND original IS NULL AND content IS NULL AND poster IS NULL))
);
COMMENT ON COLUMN private_moment_videos.audio_packet_sha256 IS 'SHA256 of ordered AAC packet SHA256 sequence; full payload/order/count compared before import, never an antivirus verdict';
CREATE INDEX private_moment_video_owner ON private_moment_videos(owner_account_id,moment_id,source_position,id);
CREATE TABLE legacy_private_video_import_items(
 run_id uuid NOT NULL REFERENCES legacy_private_video_import_runs(run_id),source_note_id uuid NOT NULL,source_media_id uuid NOT NULL,
 source_note_sha256 text NOT NULL CHECK(source_note_sha256~'^[0-9a-f]{64}$'),source_media_sha256 text NOT NULL CHECK(source_media_sha256~'^[0-9a-f]{64}$'),
 source_input_sha256 text NOT NULL CHECK(source_input_sha256~'^[0-9a-f]{64}$'),source_input_bytes bigint NOT NULL CHECK(source_input_bytes BETWEEN 1 AND 67108864),
 source_position integer NOT NULL CHECK(source_position>=0),owner_account_id uuid NOT NULL REFERENCES accounts(id),moment_id uuid NOT NULL REFERENCES moments(id),
 video_id uuid NOT NULL UNIQUE REFERENCES private_moment_videos(id),
 content_sha256 text NOT NULL CHECK(content_sha256~'^[0-9a-f]{64}$'),content_bytes bigint NOT NULL CHECK(content_bytes BETWEEN 1 AND 67108864),
 poster_sha256 text NOT NULL CHECK(poster_sha256~'^[0-9a-f]{64}$'),poster_bytes bigint NOT NULL CHECK(poster_bytes BETWEEN 1 AND 5242880),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(source_note_id,source_media_id)
);
CREATE FUNCTION birdtie_legacy_private_video_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE vid uuid;
BEGIN
 IF TG_TABLE_NAME='private_moment_videos' THEN vid:=NEW.id; ELSE vid:=NEW.video_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM legacy_private_video_import_items i
 JOIN legacy_native_adaptation_entities x ON x.run_id=i.run_id AND x.kind='moment' AND x.source_key=i.source_note_id::text AND x.target_id=i.moment_id::text AND x.owner_account_id=i.owner_account_id
 JOIN legacy_note_archives n ON n.source_note_id=i.source_note_id AND n.run_id=x.run_id AND n.owner_account_id=x.owner_account_id AND n.source_sha256=x.source_sha256 AND n.source_sha256=i.source_note_sha256 AND NOT n.deleted
 JOIN legacy_entity_mappings u ON u.run_id=n.run_id AND u.source_system=n.source_system AND u.entity_kind='user' AND u.target_id=n.owner_account_id AND u.owner_account_id=n.owner_account_id
 JOIN private_moment_videos v ON v.id=i.video_id AND v.run_id=i.run_id AND v.owner_account_id=i.owner_account_id AND v.moment_id=i.moment_id
 JOIN moments m ON m.id=v.moment_id AND m.author_account_id=v.owner_account_id
 JOIN accounts a ON a.id=m.author_account_id AND a.account_type='person' AND a.status IN('active','suspended')
 JOIN media_assets asset ON asset.id=v.id AND asset.owner_account_id=v.owner_account_id
 WHERE i.video_id=vid
 AND m.status='draft' AND m.visibility='private' AND v.status='ready_private'
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(n.source_relationships->'media') rel WHERE rel->>'sourceMediaId'=i.source_media_id::text AND rel->>'position'=i.source_position::text)
 AND v.source_position=i.source_position AND v.input_sha256=i.source_input_sha256 AND v.input_size=i.source_input_bytes
 AND encode(sha256(v.original),'hex')=i.source_input_sha256 AND encode(sha256(v.content),'hex')=i.content_sha256 AND encode(sha256(v.poster),'hex')=i.poster_sha256
 AND v.content_sha256=i.content_sha256 AND v.content_size=i.content_bytes AND v.poster_sha256=i.poster_sha256 AND v.poster_size=i.poster_bytes
 AND asset.state='ready_private' AND asset.mime_type='video/mp4' AND asset.sha256_hex=v.content_sha256 AND asset.byte_size=v.content_size) THEN
 RAISE EXCEPTION 'authenticated original owner and complete private video required';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER private_video_import_binding AFTER INSERT ON private_moment_videos DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_video_binding();
CREATE CONSTRAINT TRIGGER legacy_private_video_item_binding AFTER INSERT ON legacy_private_video_import_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_video_binding();
CREATE FUNCTION birdtie_keep_private_video_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.run_id,NEW.owner_account_id,NEW.moment_id,NEW.source_position,NEW.input_size,NEW.input_sha256,NEW.content_size,NEW.content_sha256,NEW.poster_size,NEW.poster_sha256,NEW.width,NEW.height,NEW.duration_ms,NEW.frame_count,NEW.audio_codec,NEW.audio_packet_count,NEW.audio_packet_sha256,NEW.validation_kind,NEW.antivirus_scan,NEW.privacy_risk,NEW.retain_until,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.run_id,OLD.owner_account_id,OLD.moment_id,OLD.source_position,OLD.input_size,OLD.input_sha256,OLD.content_size,OLD.content_sha256,OLD.poster_size,OLD.poster_sha256,OLD.width,OLD.height,OLD.duration_ms,OLD.frame_count,OLD.audio_codec,OLD.audio_packet_count,OLD.audio_packet_sha256,OLD.validation_kind,OLD.antivirus_scan,OLD.privacy_risk,OLD.retain_until,OLD.created_at)
 OR OLD.status='deleted' OR NEW.status<>'deleted' OR NEW.revision<>OLD.revision+1 OR NEW.original IS NOT NULL OR NEW.content IS NOT NULL OR NEW.poster IS NOT NULL
 THEN RAISE EXCEPTION 'private video identity immutable; deletion cannot resurrect bytes';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER private_video_identity BEFORE UPDATE ON private_moment_videos FOR EACH ROW EXECUTE FUNCTION birdtie_keep_private_video_identity();
CREATE FUNCTION birdtie_private_video_moment_retire() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.author_account_id IS DISTINCT FROM OLD.author_account_id OR NEW.visibility IS DISTINCT FROM OLD.visibility OR NEW.status IS DISTINCT FROM OLD.status THEN
 WITH retired AS(UPDATE private_moment_videos SET status='deleted',revision=revision+1,original=NULL,content=NULL,poster=NULL,updated_at=clock_timestamp() WHERE moment_id=OLD.id AND status='ready_private' RETURNING id)
 UPDATE media_assets asset SET state='deleted',deleted_at=clock_timestamp() FROM retired WHERE asset.id=retired.id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER private_video_moment_retire AFTER UPDATE OF author_account_id,visibility,status ON moments FOR EACH ROW EXECUTE FUNCTION birdtie_private_video_moment_retire();
CREATE FUNCTION birdtie_legacy_private_video_counts() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual jsonb;
BEGIN
 SELECT jsonb_build_object('videos',count(*),'notes',count(DISTINCT source_note_id),'owners',count(DISTINCT owner_account_id)) INTO actual FROM legacy_private_video_import_items WHERE run_id=NEW.run_id;
 IF actual<>NEW.counts OR(actual->>'videos')::bigint<1 THEN RAISE EXCEPTION 'complete private video import required';END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_private_video_counts AFTER INSERT ON legacy_private_video_import_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_private_video_counts();
CREATE TRIGGER legacy_private_video_run_immutable BEFORE UPDATE OR DELETE ON legacy_private_video_import_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_private_video_item_immutable BEFORE UPDATE OR DELETE ON legacy_private_video_import_items FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_private_video_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'private-moment-video-v1'::text $$;
COMMIT;
