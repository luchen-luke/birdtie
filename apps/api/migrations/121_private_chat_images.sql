-- Private chat image receipts and mutually exclusive audio/image attachments.
BEGIN;
CREATE TABLE human_conversation_images (
 message_id uuid PRIMARY KEY REFERENCES conversation_messages(id) ON DELETE CASCADE,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 recipient_account_id uuid NOT NULL REFERENCES accounts(id),
 conversation_id uuid NOT NULL REFERENCES conversations(id),
 operation_id uuid NOT NULL,
 purpose text NOT NULL CHECK(purpose='HUMAN_CONVERSATION_IMAGE_ATTACHMENT'),
 pixel_risk text NOT NULL CHECK(pixel_risk IN ('UNKNOWN','USER_MASKED')),
 input_mime_type text NOT NULL CHECK(input_mime_type IN ('image/png','image/jpeg')),
 input_size integer NOT NULL CHECK(input_size BETWEEN 1 AND 10485760),
 input_sha256 text NOT NULL CHECK(input_sha256 ~ '^[0-9a-f]{64}$'),
 caption_present boolean NOT NULL,
 caption_sha256 text NOT NULL CHECK(caption_sha256 ~ '^[0-9a-f]{64}$'),
 mime_type text NOT NULL CHECK(mime_type IN ('image/png','image/jpeg')),
 byte_size integer NOT NULL CHECK(byte_size BETWEEN 1 AND 16777216),
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 8192),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 8192),
 content bytea NOT NULL,
 thumbnail_byte_size integer NOT NULL CHECK(thumbnail_byte_size BETWEEN 1 AND 1048576),
 thumbnail_sha256 text NOT NULL CHECK(thumbnail_sha256 ~ '^[0-9a-f]{64}$'),
 thumbnail_width integer NOT NULL CHECK(thumbnail_width BETWEEN 1 AND 256),
 thumbnail_height integer NOT NULL CHECK(thumbnail_height BETWEEN 1 AND 256),
 thumbnail_content bytea NOT NULL,
 created_at timestamptz NOT NULL CHECK(isfinite(created_at)),
 UNIQUE(owner_account_id,operation_id),
 CHECK(owner_account_id<>recipient_account_id),
 CHECK(width::bigint*height::bigint<=16777216),
 CHECK(mime_type=input_mime_type),
 CHECK(thumbnail_width=CASE WHEN greatest(width,height)<=256 THEN width WHEN width>=height THEN 256 ELSE greatest(1,(width*256+height/2)/height) END),
 CHECK(thumbnail_height=CASE WHEN greatest(width,height)<=256 THEN height WHEN height>=width THEN 256 ELSE greatest(1,(height*256+width/2)/width) END),
 CHECK(octet_length(content)=byte_size AND encode(pg_catalog.sha256(content),'hex')=sha256),
 CHECK(octet_length(thumbnail_content)=thumbnail_byte_size AND encode(pg_catalog.sha256(thumbnail_content),'hex')=thumbnail_sha256)
);

-- Table constraints are not authentication. Store must still check actual
-- session, bilateral block, active participants and current message policy.
CREATE FUNCTION birdtie_private_chat_image_shape() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE original public.conversation_messages%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN
  IF EXISTS(SELECT 1 FROM public.conversation_messages WHERE id=OLD.message_id) THEN
   RAISE EXCEPTION 'cannot orphan image from original message' USING ERRCODE='55000';
  END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'image message receipt is immutable' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO original FROM public.conversation_messages WHERE id=NEW.message_id FOR UPDATE;
 IF NOT FOUND OR original.sender_account_id<>NEW.owner_account_id
  OR original.conversation_id<>NEW.conversation_id
  OR original.client_operation_id IS DISTINCT FROM NEW.operation_id
  OR original.speaker_kind<>'human'
  OR original.entity_type IS NOT NULL OR original.entity_id IS NOT NULL
  OR original.created_at<>NEW.created_at
  OR octet_length(original.body) NOT BETWEEN 1 AND 2000
  OR (NOT NEW.caption_present AND original.body<>'图片消息')
  OR NEW.caption_sha256<>encode(pg_catalog.sha256(convert_to(
     CASE WHEN NEW.caption_present THEN original.body ELSE '' END,'UTF8')),'hex')
  OR NOT EXISTS(SELECT 1 FROM public.conversations c WHERE c.id=NEW.conversation_id
    AND NEW.owner_account_id IN (c.member_a_account_id,c.member_b_account_id)
    AND NEW.recipient_account_id=CASE WHEN c.member_a_account_id=NEW.owner_account_id
      THEN c.member_b_account_id ELSE c.member_a_account_id END)
  OR EXISTS(SELECT 1 FROM public.human_conversation_audio a WHERE a.message_id=NEW.message_id)
 THEN
  RAISE EXCEPTION 'image must bind its original exclusive human message' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER human_conversation_images_immutable BEFORE INSERT OR UPDATE OR DELETE ON human_conversation_images
 FOR EACH ROW EXECUTE FUNCTION birdtie_private_chat_image_shape();

-- Complement the already-applied 120 audio trigger, without rewriting it.
-- Both media insertions lock the same parent before the opposite-table check;
-- caption='语音消息' cannot create two media kinds on one original message.
CREATE FUNCTION birdtie_chat_audio_no_image() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
 PERFORM 1 FROM public.conversation_messages WHERE id=NEW.message_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM public.human_conversation_images i WHERE i.message_id=NEW.message_id) THEN
  RAISE EXCEPTION 'one human message cannot have audio and image' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER human_conversation_audio_image_exclusion BEFORE INSERT OR UPDATE ON human_conversation_audio
 FOR EACH ROW EXECUTE FUNCTION birdtie_chat_audio_no_image();
COMMIT;

-- REQUIRED accompanying application edit, not a SQL migration:
-- readHumanMessageOperation must additionally exclude image rows.
-- Existing shared sender account lock + any existing owner/op check remain
-- mandatory across text/audio/image/entity; no new duplicate effect ledger.
