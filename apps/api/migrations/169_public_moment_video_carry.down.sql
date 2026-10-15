BEGIN;
DROP FUNCTION birdtie_public_moment_video_carry_contract();
CREATE OR REPLACE FUNCTION birdtie_guard_public_video_payload() RETURNS trigger LANGUAGE plpgsql AS $$
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
-- The original fresh-stage-only guard is restored before removing provenance;
-- public bytes/identities and original operation facts remain untouched.
ALTER TABLE public_moment_media_video_payloads DROP COLUMN retired_xact_id;
COMMIT;
