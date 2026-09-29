-- Group and public Intent publication is owner-confirmed. Keep former review
-- queue records private; owners may submit again under the new policy.
UPDATE communities SET publication_status = 'hidden', updated_at = now()
WHERE publication_status = 'draft' AND owner_confirmed_at IS NOT NULL;
UPDATE intents SET state = 'withdrawn', updated_at = now()
WHERE state = 'draft' AND audience = 'public' AND owner_confirmed_at IS NOT NULL;

ALTER TABLE communities DROP CONSTRAINT IF EXISTS community_reviewed_publication;
ALTER TABLE communities DROP CONSTRAINT IF EXISTS community_public_confirmation;
ALTER TABLE communities ADD CONSTRAINT community_public_confirmation CHECK (
    publication_status <> 'published' OR
    (visibility = 'public' AND owner_confirmed_at IS NOT NULL)
);
ALTER TABLE intents DROP CONSTRAINT IF EXISTS intent_independent_publication;
DROP INDEX IF EXISTS communities_review_queue;
DROP INDEX IF EXISTS intents_review_queue;
