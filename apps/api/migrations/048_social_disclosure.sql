BEGIN;

-- Public objects do not imply public attendance or membership. No legacy
-- account receives disclosure consent by migration.
CREATE TABLE person_social_disclosure (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    mutual_ties boolean NOT NULL DEFAULT false,
    shared_communities boolean NOT NULL DEFAULT false,
    shared_activities boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION birdtie_social_disclosure_person() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'social disclosure requires active Person';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER social_disclosure_person BEFORE INSERT OR UPDATE ON person_social_disclosure
    FOR EACH ROW EXECUTE FUNCTION birdtie_social_disclosure_person();

COMMIT;
