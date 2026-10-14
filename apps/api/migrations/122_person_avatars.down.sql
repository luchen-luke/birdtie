DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM person_avatars) OR EXISTS(SELECT 1 FROM person_avatar_operations) THEN
  RAISE EXCEPTION 'person avatar data exists; preserve/export it before rollback';
 END IF;
END $$;
DROP FUNCTION birdtie_person_avatar_metadata(uuid,uuid);
DROP TABLE person_avatar_operations;
DROP TABLE person_avatars;
