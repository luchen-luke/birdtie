-- Editorial membership is granted by an operator until organization roles exist.
CREATE TABLE IF NOT EXISTS city_editor_memberships (
    city_id text NOT NULL REFERENCES cities(id),
    account_id uuid NOT NULL REFERENCES accounts(id),
    role text NOT NULL CHECK (role IN ('contributor', 'reviewer')),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'revoked')),
    granted_by uuid REFERENCES accounts(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    PRIMARY KEY (city_id, account_id)
);

-- Submissions are not part of the public City Graph until reviewed.
CREATE TABLE IF NOT EXISTS place_candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    city_id text NOT NULL REFERENCES cities(id),
    submitted_by uuid NOT NULL REFERENCES accounts(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 150),
    category_code text NOT NULL,
    summary text NOT NULL DEFAULT '',
    latitude double precision,
    longitude double precision,
    location_precision text NOT NULL
        CHECK (location_precision IN ('none', 'city', 'area', 'point')),
    source_label text NOT NULL,
    source_url text NOT NULL,
    rights_note text NOT NULL,
    provider_code text,
    provider_place_id text,
    attribution text,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'published', 'linked_duplicate', 'rejected')),
    reviewed_by uuid REFERENCES accounts(id),
    reviewed_at timestamptz,
    review_note text,
    resolved_place_id uuid REFERENCES places(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT candidate_expiry CHECK (expires_at > created_at),
    CONSTRAINT candidate_provider_pair CHECK (
        (provider_code IS NULL AND provider_place_id IS NULL AND attribution IS NULL) OR
        (provider_code IS NOT NULL AND provider_place_id IS NOT NULL AND attribution IS NOT NULL)
    ),
    CONSTRAINT candidate_coordinate_pair CHECK (
        (latitude IS NULL AND longitude IS NULL) OR
        (latitude IS NOT NULL AND longitude IS NOT NULL)
    ),
    CONSTRAINT candidate_latitude_range CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CONSTRAINT candidate_longitude_range CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    CONSTRAINT candidate_precision_coordinates CHECK (
        (location_precision = 'none' AND latitude IS NULL) OR
        (location_precision <> 'none' AND latitude IS NOT NULL)
    ),
    CONSTRAINT candidate_review_state CHECK (
        (status = 'pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND resolved_place_id IS NULL) OR
        (status = 'rejected' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND resolved_place_id IS NULL) OR
        (status IN ('published', 'linked_duplicate') AND reviewed_by IS NOT NULL
            AND reviewed_at IS NOT NULL AND resolved_place_id IS NOT NULL)
    )
);
CREATE INDEX IF NOT EXISTS place_candidates_city_status
    ON place_candidates (city_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS place_candidates_pending_provider
    ON place_candidates (provider_code, provider_place_id)
    WHERE status = 'pending' AND provider_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS place_sources (
    place_id uuid NOT NULL REFERENCES places(id),
    candidate_id uuid NOT NULL UNIQUE REFERENCES place_candidates(id),
    source_label text NOT NULL,
    source_url text NOT NULL,
    rights_note text NOT NULL,
    reviewer_account_id uuid NOT NULL REFERENCES accounts(id),
    verified_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (place_id, candidate_id)
);

CREATE TABLE IF NOT EXISTS city_seed_items (
    city_id text NOT NULL REFERENCES cities(id),
    place_id uuid NOT NULL REFERENCES places(id),
    maintainer_account_id uuid NOT NULL REFERENCES accounts(id),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'review_needed', 'removed')),
    valid_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (city_id, place_id)
);
