BEGIN;

-- V4 social intent is separate from the legacy city-only public intents table.
-- No legacy row or AgentTask is silently converted into a social declaration.
CREATE TABLE social_intents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    intent_type text NOT NULL CHECK (intent_type IN
        ('FIND_ACTIVITY','FIND_COMPANION','ORGANIZE','ASK_HELP','OTHER')),
    title text NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 160),
    constraints jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(constraints)='object' AND length(constraints::text)<=4096),
    audience text NOT NULL CHECK (audience IN
        ('PRIVATE','FRIENDS','COMMUNITY','LOCAL','PUBLIC','INVITE_ONLY')),
    modality text NOT NULL CHECK (modality IN ('IN_PERSON','ONLINE','HYBRID')),
    context_id uuid REFERENCES contexts(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN
        ('DRAFT','ACTIVE','MATCHED','CONVERTED','EXPIRED','CANCELLED')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT social_intent_expiry CHECK (expires_at>created_at)
);
CREATE INDEX social_intents_creator_recent
    ON social_intents(creator_account_id,created_at DESC,id DESC);
CREATE INDEX social_intents_active_expiry
    ON social_intents(expires_at,id) WHERE status='ACTIVE';

CREATE OR REPLACE FUNCTION birdtie_social_intent_requires_person() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.creator_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'social intent requires active person creator';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER social_intent_requires_person BEFORE INSERT OR UPDATE OF creator_account_id
    ON social_intents FOR EACH ROW EXECUTE FUNCTION birdtie_social_intent_requires_person();

COMMIT;
