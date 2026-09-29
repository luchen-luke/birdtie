-- Birdtie-owned Agent task history and public group discovery. No data is seeded.
CREATE TABLE IF NOT EXISTS communities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    city_id text NOT NULL REFERENCES cities(id),
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    place_id uuid,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 160),
    summary text NOT NULL DEFAULT '' CHECK (length(summary) <= 3000),
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'public')),
    publication_status text NOT NULL DEFAULT 'draft'
        CHECK (publication_status IN ('draft', 'published', 'hidden')),
    owner_confirmed_at timestamptz,
    source_label text NOT NULL,
    source_ref text NOT NULL,
    maintainer_label text NOT NULL,
    verified_at timestamptz,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT community_source_present CHECK (
        length(source_label) > 0 AND length(source_ref) > 0 AND length(maintainer_label) > 0
    ),
    CONSTRAINT community_public_confirmation CHECK (
        publication_status <> 'published' OR
        (visibility = 'public' AND owner_confirmed_at IS NOT NULL AND verified_at IS NOT NULL)
    )
);
CREATE INDEX IF NOT EXISTS communities_public_city
    ON communities (city_id, name, id)
    WHERE publication_status = 'published' AND visibility = 'public';

-- Query and City are kept. Results are re-evaluated against current ACL on restore.
CREATE TABLE IF NOT EXISTS agent_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    city_id text NOT NULL REFERENCES cities(id),
    query text NOT NULL CHECK (length(query) BETWEEN 1 AND 240),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS agent_tasks_owner_recent
    ON agent_tasks (owner_account_id, updated_at DESC, id DESC);
