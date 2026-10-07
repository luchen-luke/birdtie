BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM sponsored_opportunity_declarations) OR EXISTS(SELECT 1 FROM sponsored_opportunity_review_grants) THEN
  RAISE EXCEPTION 'nonempty sponsorship declarations or grants must be retained';
 END IF;
END $$;
DROP FUNCTION birdtie_sponsor_review_snapshot(text,uuid,timestamptz);
DROP FUNCTION birdtie_sponsor_source_snapshot(uuid,uuid,text,uuid,timestamptz);
DROP TABLE sponsored_opportunity_declarations;
DROP TABLE sponsored_opportunity_review_grants;
DROP FUNCTION birdtie_sponsor_version_guard();
DROP FUNCTION birdtie_sponsor_person_guard();
COMMIT;
