-- Refuse rollback when it would discard persisted image-message effects.
BEGIN;
DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM human_conversation_images) THEN
  RAISE EXCEPTION 'refuse rollback with human image data; preserve original effects' USING ERRCODE='55000';
 END IF;
END $$;
DROP TRIGGER human_conversation_audio_image_exclusion ON human_conversation_audio;
DROP FUNCTION birdtie_chat_audio_no_image();
DROP TABLE human_conversation_images;
DROP FUNCTION birdtie_private_chat_image_shape();
COMMIT;
