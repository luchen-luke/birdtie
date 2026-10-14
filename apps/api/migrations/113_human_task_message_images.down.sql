BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM human_task_message_images) THEN
  RAISE EXCEPTION '113 down refuses to erase original Task attachment data';
 END IF;
END $$;
DROP TABLE human_task_message_images;
COMMIT;
