BEGIN;
CREATE TABLE person_agent_relationship_consent (
 account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER agent_relationship_consent_person BEFORE INSERT OR UPDATE ON person_agent_relationship_consent
 FOR EACH ROW EXECUTE FUNCTION birdtie_social_disclosure_person();
-- No prior consent, profile grant or accepted Tie is migrated into Agent consent.
COMMIT;
