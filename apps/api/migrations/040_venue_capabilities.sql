-- Optional, reviewed hosting capability for an existing Place. A Place has no Venue by default.
CREATE TABLE venue_candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    place_id uuid NOT NULL,
    city_id text NOT NULL,
    submitted_by uuid NOT NULL REFERENCES accounts(id),
    capacity integer CHECK (capacity BETWEEN 1 AND 100000),
    reservation_support text NOT NULL DEFAULT 'unknown'
        CHECK (reservation_support IN ('unknown', 'none', 'contact', 'external_url')),
    reservation_url text,
    suitability text[] NOT NULL DEFAULT '{}',
    amenities text[] NOT NULL DEFAULT '{}',
    operator_organization_id uuid REFERENCES organizations(id),
    source_url text NOT NULL CHECK (source_url ~ '^https://[^[:space:]]+$'),
    rights_note text NOT NULL CHECK (length(trim(rights_note)) BETWEEN 10 AND 1000),
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by uuid REFERENCES accounts(id),
    reviewed_at timestamptz,
    review_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT venue_candidate_reservation CHECK
        ((reservation_support = 'external_url') = (reservation_url IS NOT NULL)),
    CONSTRAINT venue_candidate_url CHECK
        (reservation_url IS NULL OR reservation_url ~ '^https://[^[:space:]]+$'),
    CONSTRAINT venue_candidate_known_fact CHECK
        (capacity IS NOT NULL OR reservation_support <> 'unknown' OR
         cardinality(suitability) > 0 OR cardinality(amenities) > 0),
    CONSTRAINT venue_candidate_array_bounds CHECK
        (cardinality(suitability) <= 20 AND cardinality(amenities) <= 20),
    CONSTRAINT venue_candidate_review_state CHECK (
        (status='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL) OR
        (status IN ('approved','rejected') AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL)
    ),
    CONSTRAINT venue_candidate_expiry CHECK (expires_at > created_at)
);
CREATE INDEX venue_candidates_city_pending ON venue_candidates(city_id, created_at, id)
    WHERE status='pending';
CREATE UNIQUE INDEX venue_candidates_one_pending ON venue_candidates(place_id)
    WHERE status='pending';

CREATE TABLE venues (
    place_id uuid PRIMARY KEY,
    city_id text NOT NULL,
    capacity integer CHECK (capacity BETWEEN 1 AND 100000),
    reservation_support text NOT NULL
        CHECK (reservation_support IN ('unknown', 'none', 'contact', 'external_url')),
    reservation_url text,
    suitability text[] NOT NULL DEFAULT '{}',
    amenities text[] NOT NULL DEFAULT '{}',
    operator_organization_id uuid REFERENCES organizations(id),
    source_candidate_id uuid NOT NULL UNIQUE REFERENCES venue_candidates(id),
    source_url text NOT NULL,
    reviewed_by uuid NOT NULL REFERENCES accounts(id),
    reviewed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT venue_reservation CHECK
        ((reservation_support = 'external_url') = (reservation_url IS NOT NULL)),
    CONSTRAINT venue_url CHECK
        (reservation_url IS NULL OR reservation_url ~ '^https://[^[:space:]]+$'),
    CONSTRAINT venue_known_fact CHECK
        (capacity IS NOT NULL OR reservation_support <> 'unknown' OR
         cardinality(suitability) > 0 OR cardinality(amenities) > 0)
);
