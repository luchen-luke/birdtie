DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM place_semantic_profiles) OR EXISTS(SELECT 1 FROM place_semantic_candidates) THEN
  RAISE EXCEPTION 'place_semantic_nonempty_down_refused';
 END IF;
END $$;
DROP TABLE place_semantic_profiles;
DROP TABLE place_semantic_candidates;
DROP FUNCTION birdtie_place_semantic_profile_source();
DROP FUNCTION birdtie_place_semantic_candidate_immutable();
DROP FUNCTION birdtie_place_semantic_facts_valid(jsonb);
