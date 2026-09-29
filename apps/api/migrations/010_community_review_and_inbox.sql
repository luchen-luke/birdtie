-- Owner-confirmed Communities require independent city review before publication.
ALTER TABLE communities ADD COLUMN IF NOT EXISTS rights_note text NOT NULL DEFAULT '';
ALTER TABLE communities ADD COLUMN IF NOT EXISTS reviewed_by uuid REFERENCES accounts(id);
ALTER TABLE communities ADD COLUMN IF NOT EXISTS reviewed_at timestamptz;
ALTER TABLE communities ADD COLUMN IF NOT EXISTS review_note text;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'community_reviewed_publication') THEN
        ALTER TABLE communities ADD CONSTRAINT community_reviewed_publication CHECK (
            publication_status <> 'published' OR
            (reviewed_by IS NOT NULL AND reviewed_by <> owner_account_id AND reviewed_at IS NOT NULL)
        );
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS communities_review_queue ON communities (city_id, created_at, id)
    WHERE publication_status = 'draft' AND owner_confirmed_at IS NOT NULL;

-- The producer is a reviewed domain event, not a client-supplied notification.
CREATE TABLE IF NOT EXISTS inbox_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    category text NOT NULL CHECK (category IN
        ('needs_attention', 'messages', 'requests', 'agent_updates', 'updates')),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    detail text NOT NULL CHECK (length(detail) BETWEEN 1 AND 500),
    resource_type text NOT NULL CHECK (resource_type IN
        ('place_candidate', 'activity_candidate', 'community')),
    resource_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz,
    UNIQUE (recipient_account_id, resource_type, resource_id)
);
CREATE INDEX IF NOT EXISTS inbox_items_owner_recent
    ON inbox_items (recipient_account_id, created_at DESC, id DESC);
