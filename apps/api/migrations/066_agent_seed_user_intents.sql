BEGIN;
-- The sole new seed authority is a private, explicitly declared UserIntent.
-- Profile, CITY/current, language and interest values retain their old sources.
CREATE TABLE agent_seed_user_intents (
 agent_id uuid PRIMARY KEY,
 owner_id uuid NOT NULL,
 owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
 version bigint NOT NULL CHECK(version>0 AND version<9223372036854775807),
 basic_intent text CHECK(basic_intent IN ('FIND_PEOPLE','FIND_ACTIVITIES','EXPLORE_CITY','SIMILAR_INTERESTS','JOIN_COMMUNITIES','DISCOVER_PLACES','JUST_EXPLORE')),
 progress text NOT NULL CHECK(progress IN ('DEFERRED','COMPLETED')),
 interest_choice text NOT NULL CHECK(interest_choice IN ('KEEP','SKIP','SET')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT agent_seed_person_binding FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
 CONSTRAINT agent_seed_completed_intent CHECK(progress<>'COMPLETED' OR basic_intent IS NOT NULL),
 CONSTRAINT agent_seed_finite_time CHECK(isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at)
);
CREATE FUNCTION birdtie_guard_agent_seed_user_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.agent_id IS DISTINCT FROM OLD.agent_id OR NEW.owner_id IS DISTINCT FROM OLD.owner_id OR NEW.owner_type IS DISTINCT FROM OLD.owner_type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
   RAISE EXCEPTION 'seed binding is immutable';
  END IF;
  IF NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'seed requires next source version'; END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_seed_user_intent_guard BEFORE INSERT OR UPDATE ON agent_seed_user_intents FOR EACH ROW EXECUTE FUNCTION birdtie_guard_agent_seed_user_intent();
COMMIT;
