-- Owner-only bookmarks reference canonical City Graph objects. Visibility is
-- rechecked on every read; a bookmark does not grant access to its target.
CREATE TABLE IF NOT EXISTS saved_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    place_id uuid REFERENCES places(id) ON DELETE CASCADE,
    activity_id uuid REFERENCES activities(id) ON DELETE CASCADE,
    community_id uuid REFERENCES communities(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT saved_item_one_target CHECK (
        num_nonnulls(place_id, activity_id, community_id) = 1
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS saved_items_owner_place
    ON saved_items(owner_account_id, place_id) WHERE place_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS saved_items_owner_activity
    ON saved_items(owner_account_id, activity_id) WHERE activity_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS saved_items_owner_community
    ON saved_items(owner_account_id, community_id) WHERE community_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS saved_items_owner_recent
    ON saved_items(owner_account_id, created_at DESC, id DESC);
