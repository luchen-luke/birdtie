BEGIN;
-- 152 retains the normalized JPEG poster/thumbnail and the common current
-- collection slot. These additive children hold only canonical MP4 bytes.
CREATE TABLE moment_media_video_materials (
 media_id uuid PRIMARY KEY REFERENCES moment_media_materials(id) ON DELETE CASCADE,
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 24 AND 20971520),
 sha256 char(64) NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 contract jsonb NOT NULL,
 CHECK(encode(sha256(content),'hex')=sha256)
);
CREATE TABLE public_moment_media_video_payloads (
 moment_id uuid NOT NULL,set_version bigint NOT NULL,position integer NOT NULL CHECK(position=0),
 media_id uuid NOT NULL,media_version bigint NOT NULL,content bytea,
 sha256 char(64) NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),contract jsonb NOT NULL,
 PRIMARY KEY(moment_id,set_version,position),
 FOREIGN KEY(moment_id,set_version,position) REFERENCES public_moment_media_attachments(moment_id,set_version,position),
 CHECK(content IS NULL OR (octet_length(content) BETWEEN 24 AND 20971520 AND encode(sha256(content),'hex')=sha256))
);
CREATE FUNCTION birdtie_guard_canonical_video_material() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE material record;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'canonical video stage is immutable';END IF;
 SELECT * INTO material FROM moment_media_materials WHERE id=NEW.media_id FOR SHARE;
 IF NOT FOUND OR material.retain_until<=clock_timestamp()
 OR material.metadata->>'schemaVersion' IS DISTINCT FROM 'public-moment-media-stage-v2'
 OR material.metadata->>'kind' IS DISTINCT FROM 'VIDEO'
 OR material.metadata->>'purpose' IS DISTINCT FROM 'PUBLIC_MOMENT_VIDEO_STAGE_V2'
 OR material.metadata->>'validationKind' IS DISTINCT FROM 'FFMPEG_TRANSCODE_REPROBE_FULL_DECODE_V2'
 OR material.metadata->'video' IS DISTINCT FROM NEW.contract
 OR NEW.contract->>'videoSha256' IS DISTINCT FROM NEW.sha256::text
 OR (NEW.contract->>'videoBytes')::integer IS DISTINCT FROM octet_length(NEW.content)
 OR NEW.contract->>'codecVideo' IS DISTINCT FROM 'h264'
 OR COALESCE(NEW.contract->>'codecAudio','') NOT IN('aac','none')
 OR NEW.contract->>'pixelFormat' IS DISTINCT FROM 'yuv420p'
 OR NEW.contract->>'mimeType' IS DISTINCT FROM 'video/mp4'
 OR COALESCE((NEW.contract->>'durationMs')::bigint,0) NOT BETWEEN 1 AND 30000 THEN
 RAISE EXCEPTION 'matching canonical validated video stage required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER canonical_video_material_guard BEFORE INSERT OR UPDATE ON moment_media_video_materials FOR EACH ROW EXECUTE FUNCTION birdtie_guard_canonical_video_material();
CREATE FUNCTION birdtie_guard_public_video_payload() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE attachment record; slot record;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'video payload identity retained';END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW.content IS NOT NULL OR (to_jsonb(NEW)-'content') IS DISTINCT FROM (to_jsonb(OLD)-'content') THEN RAISE EXCEPTION 'only video byte retirement allowed';END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO attachment FROM public_moment_media_attachments WHERE moment_id=NEW.moment_id AND set_version=NEW.set_version AND position=NEW.position;
 SELECT * INTO slot FROM public_moment_media_sets WHERE moment_id=NEW.moment_id;
 IF attachment.media_id IS DISTINCT FROM NEW.media_id OR attachment.media_version IS DISTINCT FROM NEW.media_version
 OR slot.status IS DISTINCT FROM 'READY' OR slot.version IS DISTINCT FROM NEW.set_version
 OR attachment.display IS NULL OR attachment.metadata->>'kind' IS DISTINCT FROM 'VIDEO'
 OR attachment.metadata->'video' IS DISTINCT FROM NEW.contract
 OR NEW.contract->>'videoSha256' IS DISTINCT FROM NEW.sha256::text
 OR NOT EXISTS(SELECT 1 FROM moment_media_video_materials v JOIN moment_media_materials m ON m.id=v.media_id
 WHERE v.media_id=NEW.media_id AND m.version=NEW.media_version AND m.owner_account_id=slot.owner_account_id
 AND m.retain_until>clock_timestamp() AND v.content=NEW.content AND v.sha256=NEW.sha256)
 THEN RAISE EXCEPTION 'exact approved canonical video copy required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER public_video_payload_guard BEFORE INSERT OR UPDATE OR DELETE ON public_moment_media_video_payloads FOR EACH ROW EXECUTE FUNCTION birdtie_guard_public_video_payload();
CREATE FUNCTION birdtie_retire_public_video_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.display IS NULL OR NEW.thumbnail IS NULL THEN
 UPDATE public_moment_media_video_payloads SET content=NULL WHERE moment_id=NEW.moment_id AND set_version=NEW.set_version AND position=NEW.position AND content IS NOT NULL;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER public_video_bytes_retire AFTER UPDATE OF display,thumbnail ON public_moment_media_attachments FOR EACH ROW EXECUTE FUNCTION birdtie_retire_public_video_payload();
CREATE FUNCTION birdtie_check_video_collection_kind() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tid uuid;ver bigint;n integer;videos integer;
BEGIN
 tid:=NEW.moment_id;ver:=NEW.set_version;
 SELECT count(*),count(*) FILTER(WHERE metadata->>'kind'='VIDEO') INTO n,videos
 FROM public_moment_media_attachments WHERE moment_id=tid AND set_version=ver;
 IF videos>0 AND (videos<>1 OR n<>1) THEN RAISE EXCEPTION 'nine images or one video, never mixed';END IF;
 IF NEW.metadata->>'kind'='VIDEO' AND NOT EXISTS(SELECT 1 FROM public_moment_media_video_payloads v WHERE v.moment_id=tid AND v.set_version=ver AND v.position=0) THEN
 RAISE EXCEPTION 'complete video payload required';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER public_media_typed_kind AFTER INSERT ON public_moment_media_attachments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_video_collection_kind();
CREATE FUNCTION birdtie_public_moment_video_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'public-moment-video-v2'::text $$;
COMMIT;
