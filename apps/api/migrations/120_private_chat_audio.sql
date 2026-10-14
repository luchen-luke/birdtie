BEGIN;
-- Private human conversation audio only. No Agent/context/provider projection.
CREATE TABLE human_conversation_audio (
 message_id uuid PRIMARY KEY REFERENCES conversation_messages(id) ON DELETE CASCADE,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 conversation_id uuid NOT NULL REFERENCES conversations(id),
 operation_id uuid NOT NULL,
 mime_type text NOT NULL CHECK(mime_type='audio/mp4'),
 byte_size integer NOT NULL CHECK(byte_size BETWEEN 1 AND 1048576),
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 duration_millis integer NOT NULL CHECK(duration_millis BETWEEN 1 AND 90000),
 content bytea NOT NULL,
 created_at timestamptz NOT NULL CHECK(isfinite(created_at)),
 UNIQUE(owner_account_id,operation_id),
 CHECK(octet_length(content)=byte_size AND encode(pg_catalog.sha256(content),'hex')=sha256)
);
CREATE FUNCTION birdtie_private_chat_audio_shape() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'audio message receipt is immutable' USING ERRCODE='55000'; END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM conversation_messages m WHERE m.id=NEW.message_id
  AND m.sender_account_id=NEW.owner_account_id AND m.conversation_id=NEW.conversation_id
  AND m.client_operation_id=NEW.operation_id AND m.speaker_kind='human'
  AND m.entity_type IS NULL AND m.entity_id IS NULL AND m.body='语音消息'
  AND m.created_at=NEW.created_at) THEN
  RAISE EXCEPTION 'audio message must bind its original human effect' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER human_conversation_audio_immutable BEFORE INSERT OR UPDATE ON human_conversation_audio
 FOR EACH ROW EXECUTE FUNCTION birdtie_private_chat_audio_shape();
COMMIT;
