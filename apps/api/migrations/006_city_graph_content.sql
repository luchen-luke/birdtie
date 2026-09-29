-- Birdtie City Graph objects. Content is private/draft by default; this
-- migration creates no user content or public Activity/Moment/Journey/Intent.

-- Composite keys prevent a content relation from pairing a Place with the
-- wrong City. The existing Place UUID remains the canonical identifier.
CREATE UNIQUE INDEX IF NOT EXISTS places_id_city_unique ON places (id, city_id);

CREATE TABLE IF NOT EXISTS moments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_account_id uuid NOT NULL REFERENCES accounts(id),
    city_id text NOT NULL REFERENCES cities(id),
    place_id uuid,
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    body text NOT NULL DEFAULT '' CHECK (length(body) <= 10000),
    occurred_at timestamptz,
    time_precision text NOT NULL DEFAULT 'unknown'
        CHECK (time_precision IN ('unknown', 'year', 'month', 'day', 'instant')),
    location_precision text NOT NULL DEFAULT 'city'
        CHECK (location_precision IN ('none', 'city', 'place')),
    visibility text NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public')),
    status text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'withdrawn')),
    author_confirmed_at timestamptz,
    published_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT moment_time_precision CHECK (
        (time_precision = 'unknown' AND occurred_at IS NULL) OR
        (time_precision <> 'unknown' AND occurred_at IS NOT NULL)
    ),
    CONSTRAINT moment_location_precision CHECK (
        location_precision <> 'place' OR place_id IS NOT NULL
    ),
    CONSTRAINT moment_public_confirmation CHECK (
        status <> 'published' OR
        (visibility = 'public' AND author_confirmed_at IS NOT NULL AND published_at IS NOT NULL)
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS moments_id_city_unique ON moments (id, city_id);
CREATE INDEX IF NOT EXISTS moments_public_city_time ON moments (city_id, published_at DESC, id)
    WHERE status = 'published' AND visibility = 'public';
CREATE INDEX IF NOT EXISTS moments_author_status ON moments (author_account_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS activities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    host_account_id uuid NOT NULL REFERENCES accounts(id),
    city_id text NOT NULL REFERENCES cities(id),
    place_id uuid,
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    summary text NOT NULL DEFAULT '' CHECK (length(summary) <= 3000),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    time_zone text NOT NULL,
    publication_status text NOT NULL DEFAULT 'draft'
        CHECK (publication_status IN ('draft', 'published', 'hidden')),
    cancelled_at timestamptz,
    source_label text NOT NULL,
    source_ref text NOT NULL,
    maintainer_label text NOT NULL,
    verified_at timestamptz,
    expires_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT activity_time_order CHECK (ends_at > starts_at),
    CONSTRAINT activity_source_present CHECK (
        length(source_label) > 0 AND length(source_ref) > 0 AND length(maintainer_label) > 0
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS activities_id_city_unique ON activities (id, city_id);
CREATE INDEX IF NOT EXISTS activities_public_city_time ON activities (city_id, starts_at, id)
    WHERE publication_status = 'published';

CREATE TABLE IF NOT EXISTS journeys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    city_id text NOT NULL REFERENCES cities(id),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    summary text NOT NULL DEFAULT '' CHECK (length(summary) <= 5000),
    visibility text NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public')),
    status text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'withdrawn')),
    occurred_start timestamptz,
    occurred_end timestamptz,
    inspired_by_journey_id uuid REFERENCES journeys(id),
    owner_confirmed_at timestamptz,
    published_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT journey_time_order CHECK (
        occurred_start IS NULL OR occurred_end IS NULL OR occurred_end >= occurred_start
    ),
    CONSTRAINT journey_public_confirmation CHECK (
        status <> 'published' OR
        (visibility = 'public' AND owner_confirmed_at IS NOT NULL AND published_at IS NOT NULL)
    ),
    CONSTRAINT journey_no_self_inspiration CHECK (inspired_by_journey_id IS NULL OR inspired_by_journey_id <> id)
);
CREATE UNIQUE INDEX IF NOT EXISTS journeys_id_city_unique ON journeys (id, city_id);
CREATE INDEX IF NOT EXISTS journeys_public_city_time ON journeys (city_id, published_at DESC, id)
    WHERE status = 'published' AND visibility = 'public';

CREATE TABLE IF NOT EXISTS journey_stops (
    journey_id uuid NOT NULL,
    city_id text NOT NULL,
    sequence_no integer NOT NULL CHECK (sequence_no > 0),
    place_id uuid,
    occurred_at timestamptz,
    note text NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    location_visibility text NOT NULL DEFAULT 'hidden'
        CHECK (location_visibility IN ('hidden', 'city', 'place')),
    PRIMARY KEY (journey_id, sequence_no),
    FOREIGN KEY (journey_id, city_id) REFERENCES journeys(id, city_id) ON DELETE CASCADE,
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT journey_stop_location CHECK (
        location_visibility <> 'place' OR place_id IS NOT NULL
    )
);

CREATE TABLE IF NOT EXISTS moment_activity_links (
    moment_id uuid NOT NULL,
    activity_id uuid NOT NULL,
    city_id text NOT NULL,
    author_confirmed_at timestamptz NOT NULL,
    PRIMARY KEY (moment_id, activity_id),
    FOREIGN KEY (moment_id, city_id) REFERENCES moments(id, city_id) ON DELETE CASCADE,
    FOREIGN KEY (activity_id, city_id) REFERENCES activities(id, city_id)
);
CREATE TABLE IF NOT EXISTS moment_journey_links (
    moment_id uuid NOT NULL,
    journey_id uuid NOT NULL,
    city_id text NOT NULL,
    author_confirmed_at timestamptz NOT NULL,
    PRIMARY KEY (moment_id, journey_id),
    FOREIGN KEY (moment_id, city_id) REFERENCES moments(id, city_id) ON DELETE CASCADE,
    FOREIGN KEY (journey_id, city_id) REFERENCES journeys(id, city_id)
);

CREATE TABLE IF NOT EXISTS intents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    city_id text NOT NULL REFERENCES cities(id),
    topic text NOT NULL CHECK (length(topic) BETWEEN 1 AND 160),
    details text NOT NULL DEFAULT '' CHECK (length(details) <= 3000),
    available_from timestamptz NOT NULL,
    available_until timestamptz NOT NULL,
    time_zone text NOT NULL,
    coarse_area_label text NOT NULL CHECK (length(coarse_area_label) BETWEEN 1 AND 160),
    audience text NOT NULL DEFAULT 'private'
        CHECK (audience IN ('private', 'connections', 'public')),
    state text NOT NULL DEFAULT 'draft'
        CHECK (state IN ('draft', 'active', 'fulfilled', 'withdrawn')),
    expires_at timestamptz NOT NULL,
    owner_confirmed_at timestamptz,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT intent_window CHECK (
        available_until > available_from AND expires_at > created_at
    ),
    CONSTRAINT intent_activation CHECK (
        state <> 'active' OR owner_confirmed_at IS NOT NULL
    )
);
CREATE INDEX IF NOT EXISTS intents_public_city_expiry ON intents (city_id, expires_at, id)
    WHERE state = 'active' AND audience = 'public';
CREATE INDEX IF NOT EXISTS intents_owner_state ON intents (owner_account_id, state, updated_at DESC);
