BEGIN;
CREATE TABLE person_new_people_consent (
 account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER new_people_consent_person BEFORE INSERT OR UPDATE ON person_new_people_consent
 FOR EACH ROW EXECUTE FUNCTION birdtie_social_disclosure_person();
-- No profile visibility, Agent consent, private Intent or accepted Tie is opt-in.
COMMIT;
