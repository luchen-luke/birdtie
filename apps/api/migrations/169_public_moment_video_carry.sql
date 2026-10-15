BEGIN;
-- Full transaction identity includes the xid epoch. Historical tombstones keep
-- NULL; only the non-NULL -> NULL byte transition below can mint this proof.
ALTER TABLE public_moment_media_video_payloads ADD COLUMN retired_xact_id xid8;
-- A published video is retained by its approved public collection, independently
-- of the ten-minute upload stage. A text/order edit retires that collection and
-- carries its exact bytes into the immediately following Moment revision in one
-- transaction. Neither an expired new upload nor historical retired bytes qualify.
CREATE OR REPLACE FUNCTION birdtie_guard_public_video_payload() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE attachment record; slot record;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'video payload identity retained';END IF;
 IF TG_OP='UPDATE' THEN
  -- A no-op on an already retired row must not manufacture same-transaction
  -- provenance through xmin. The normal retirement writers select non-NULL bytes.
  IF OLD.content IS NULL OR NEW.content IS NOT NULL OR NEW.retired_xact_id IS DISTINCT FROM OLD.retired_xact_id
  OR (to_jsonb(NEW)-'content'-'retired_xact_id') IS DISTINCT FROM (to_jsonb(OLD)-'content'-'retired_xact_id') THEN
   RAISE EXCEPTION 'only live video byte retirement allowed';END IF;
  NEW.retired_xact_id:=pg_current_xact_id();
  RETURN NEW;
 END IF;
 SELECT * INTO attachment FROM public_moment_media_attachments WHERE moment_id=NEW.moment_id AND set_version=NEW.set_version AND position=NEW.position;
 SELECT * INTO slot FROM public_moment_media_sets WHERE moment_id=NEW.moment_id;
 IF NEW.content IS NULL OR NEW.retired_xact_id IS NOT NULL
 OR attachment.media_id IS DISTINCT FROM NEW.media_id OR attachment.media_version IS DISTINCT FROM NEW.media_version
 OR slot.status IS DISTINCT FROM 'READY' OR slot.version IS DISTINCT FROM NEW.set_version
 OR attachment.display IS NULL OR attachment.metadata->>'kind' IS DISTINCT FROM 'VIDEO'
 OR attachment.metadata->'video' IS DISTINCT FROM NEW.contract
 OR NEW.contract->>'videoSha256' IS DISTINCT FROM NEW.sha256::text
 OR NOT (
  EXISTS(SELECT 1 FROM moment_media_video_materials v JOIN moment_media_materials m ON m.id=v.media_id
   WHERE v.media_id=NEW.media_id AND m.version=NEW.media_version AND m.owner_account_id=slot.owner_account_id
   AND m.retain_until>clock_timestamp() AND v.content=NEW.content AND v.sha256=NEW.sha256)
  OR EXISTS(
   SELECT 1 FROM public_moment_media_video_payloads prior_video
   JOIN public_moment_media_attachments prior USING(moment_id,set_version,position)
   JOIN moments m ON m.id=prior_video.moment_id
   JOIN accounts owner ON owner.id=m.author_account_id
   WHERE prior_video.moment_id=NEW.moment_id AND prior_video.position=NEW.position
   AND prior_video.set_version=NEW.set_version-2
   AND prior_video.media_id=NEW.media_id AND prior_video.media_version=NEW.media_version
   AND prior.media_id=NEW.media_id AND prior.media_version=NEW.media_version
   AND prior_video.content IS NULL AND prior.display IS NULL AND prior.thumbnail IS NULL
   AND prior_video.retired_xact_id=pg_current_xact_id()
   AND prior.xmin::text=(pg_current_xact_id()::text::numeric % 4294967296)::text
   AND m.xmin::text=(pg_current_xact_id()::text::numeric % 4294967296)::text
   AND m.author_account_id=slot.owner_account_id AND owner.account_type='person' AND owner.status='active'
   AND m.status='published' AND m.visibility='public' AND m.revision=slot.approved_moment_revision
   AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL
   AND m.author_confirmed_at<=m.published_at AND m.published_at<=clock_timestamp()
   AND prior_video.sha256=NEW.sha256
   AND prior_video.contract-'videoPath'=NEW.contract-'videoPath'
   AND prior_video.contract->>'videoPath'='/v1/moments/'||NEW.moment_id::text||'/public-media/'||(m.revision-1)::text||'/'||prior_video.set_version::text||'/'||NEW.media_id::text||'/'||NEW.media_version::text||'/video'
   AND NEW.contract->>'videoPath'='/v1/moments/'||NEW.moment_id::text||'/public-media/'||m.revision::text||'/'||NEW.set_version::text||'/'||NEW.media_id::text||'/'||NEW.media_version::text||'/video'
   AND prior.metadata->'video'=prior_video.contract
   AND prior.metadata-ARRAY['displayPath','thumbnailPath','expiresAt','video']=attachment.metadata-ARRAY['displayPath','thumbnailPath','expiresAt','video']
   AND prior.rights=attachment.rights AND prior.retention_until=attachment.retention_until
   AND attachment.retention_until>clock_timestamp()
   AND attachment.rights->>'canRetain'='true' AND attachment.rights->>'canPublish'='true'
   AND encode(sha256(attachment.display),'hex')=prior.metadata->>'displaySha256'
   AND encode(sha256(attachment.thumbnail),'hex')=prior.metadata->>'thumbnailSha256'
   AND octet_length(attachment.display)=(prior.metadata->>'displayBytes')::integer
   AND octet_length(attachment.thumbnail)=(prior.metadata->>'thumbnailBytes')::integer
  )
 ) THEN RAISE EXCEPTION 'exact approved canonical video copy required';END IF;
 -- The table CHECK independently verifies NEW.content against this immutable SHA.
 RETURN NEW;
END $$;
CREATE FUNCTION birdtie_public_moment_video_carry_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'public-moment-video-carry-v1'::text $$;
COMMIT;
