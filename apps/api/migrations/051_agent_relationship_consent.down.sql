BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM person_agent_relationship_consent) THEN
  RAISE EXCEPTION 'cannot remove Agent relationship consent while choices exist';
 END IF;
END $$;
DROP TABLE person_agent_relationship_consent;
COMMIT;
