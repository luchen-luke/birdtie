-- Ordinary personal creation. Existing Journey identities are retained;
-- no public publishing, model export or automatic precise-location sharing.
CREATE TABLE IF NOT EXISTS theme_maps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    city_id text NOT NULL REFERENCES cities(id),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    summary text NOT NULL DEFAULT '' CHECK (length(summary) <= 5000),
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility = 'private'),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','withdrawn')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(id, city_id)
);
CREATE TABLE IF NOT EXISTS theme_map_places (
    theme_map_id uuid NOT NULL,
    city_id text NOT NULL,
    sequence_no integer NOT NULL CHECK (sequence_no BETWEEN 1 AND 50),
    place_id uuid NOT NULL,
    note text NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    location_visibility text NOT NULL DEFAULT 'hidden' CHECK (location_visibility IN ('hidden','city','place')),
    PRIMARY KEY(theme_map_id, sequence_no),
    UNIQUE(theme_map_id, place_id),
    FOREIGN KEY(theme_map_id, city_id) REFERENCES theme_maps(id, city_id) ON DELETE CASCADE,
    FOREIGN KEY(place_id, city_id) REFERENCES places(id, city_id)
);
CREATE TABLE IF NOT EXISTS personal_collection_operations (
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    operation_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('journey','theme_map')),
    target_id uuid NOT NULL,
    input_sha256 bytea NOT NULL CHECK (octet_length(input_sha256)=32),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_account_id, operation_id)
);
CREATE INDEX IF NOT EXISTS theme_maps_owner_page ON theme_maps(owner_account_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS journeys_owner_page ON journeys(owner_account_id, created_at DESC, id DESC);
