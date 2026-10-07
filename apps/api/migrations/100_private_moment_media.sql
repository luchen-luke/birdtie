-- Private, human-owned Moment derivatives only. No public variant or raw EXIF.
CREATE TABLE private_moment_images (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 moment_id uuid NOT NULL REFERENCES moments(id),
 operation_id uuid NOT NULL,
 source_binding text NOT NULL,
 moment_revision bigint NOT NULL CHECK(moment_revision>0),
 session_id uuid NOT NULL REFERENCES sessions(id),
 mime_type text NOT NULL CHECK(mime_type IN ('image/jpeg','image/png')),
 input_size bigint NOT NULL CHECK(input_size>0 AND input_size<=10485760),
 input_sha256 char(64) NOT NULL CHECK(input_sha256 ~ '^[0-9a-f]{64}$'),
 pixel_risk text NOT NULL CHECK(pixel_risk IN ('UNKNOWN','USER_MASKED')),
 purpose text NOT NULL CHECK(purpose='PRIVATE_MOMENT_ATTACHMENT'),
 status text NOT NULL DEFAULT 'preview' CHECK(status IN ('preview','ready_private','deleted')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 derivative bytea,
 derivative_sha256 char(64),
 preview_expires_at timestamptz NOT NULL,
 retain_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(preview_expires_at>created_at AND retain_until>preview_expires_at),
 CHECK((status='ready_private' AND derivative IS NOT NULL AND octet_length(derivative)>0
   AND octet_length(derivative)<=16777216 AND derivative_sha256 IS NOT NULL AND derivative_sha256 ~ '^[0-9a-f]{64}$')
   OR (status IN ('preview','deleted') AND derivative IS NULL AND derivative_sha256 IS NULL)),
 UNIQUE(owner_account_id,operation_id)
);
CREATE INDEX private_moment_images_source ON private_moment_images(owner_account_id,moment_id,status);
-- Operation tombstones remain after deletion; no content resurrection/replay.
COMMENT ON TABLE private_moment_images IS 'No model, public, original image or EXIF storage. Deletion clears logical bytes; physical page erasure is not claimed.';

-- Ordinary text edits retain human-readable attachments. A visibility/status/
-- author transition retires private bytes permanently, including later ABA.
CREATE FUNCTION birdtie_retire_private_moment_images() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE image_id uuid;
BEGIN
 IF NEW.author_account_id IS DISTINCT FROM OLD.author_account_id
  OR NEW.visibility<>'private' OR NEW.status<>'draft' THEN
  FOR image_id IN SELECT id FROM private_moment_images
    WHERE moment_id=OLD.id AND status<>'deleted' ORDER BY id FOR UPDATE LOOP
   UPDATE private_moment_images SET status='deleted',derivative=NULL,
    derivative_sha256=NULL,revision=revision+1,updated_at=clock_timestamp() WHERE id=image_id;
   UPDATE media_assets SET state='deleted',deleted_at=clock_timestamp() WHERE id=image_id;
  END LOOP;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER private_moment_images_retire BEFORE UPDATE OF visibility,status,author_account_id
 ON moments FOR EACH ROW EXECUTE FUNCTION birdtie_retire_private_moment_images();
