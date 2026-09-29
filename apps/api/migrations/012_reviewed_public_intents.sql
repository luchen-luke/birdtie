-- An owner-confirmed public Intent requires a distinct city reviewer.
ALTER TABLE intents ADD COLUMN IF NOT EXISTS reviewed_by uuid REFERENCES accounts(id);
ALTER TABLE intents ADD COLUMN IF NOT EXISTS reviewed_at timestamptz;
ALTER TABLE intents ADD COLUMN IF NOT EXISTS review_note text;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'intent_independent_publication') THEN
        ALTER TABLE intents ADD CONSTRAINT intent_independent_publication CHECK (
            state <> 'active' OR audience <> 'public' OR
            (reviewed_by IS NOT NULL AND reviewed_by <> owner_account_id AND reviewed_at IS NOT NULL)
        );
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS intents_review_queue ON intents (city_id, created_at, id)
    WHERE state = 'draft' AND audience = 'public' AND owner_confirmed_at IS NOT NULL;

ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK (resource_type IN
    ('place_candidate', 'activity_candidate', 'community', 'intent'));
