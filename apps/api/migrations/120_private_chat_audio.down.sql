BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM human_conversation_audio) THEN
  RAISE EXCEPTION 'used private audio cannot be discarded by downgrade';
 END IF;
END $$;
DROP TABLE human_conversation_audio;
DROP FUNCTION birdtie_private_chat_audio_shape();
COMMIT;
