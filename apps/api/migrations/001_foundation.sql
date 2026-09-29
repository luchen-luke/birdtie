-- Birdtie-owned schema. PostgreSQL 15+; no Civu tables or production data.

CREATE TABLE IF NOT EXISTS accounts (
    id uuid PRIMARY KEY,
    account_type text NOT NULL CHECK (account_type IN ('person', 'organization')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
    handle text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS accounts_handle_unique
    ON accounts (lower(handle)) WHERE handle IS NOT NULL AND status <> 'deleted';

CREATE TABLE IF NOT EXISTS user_profiles (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    display_name text NOT NULL,
    bio text NOT NULL DEFAULT '',
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'public')),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS consent_grants (
    id uuid PRIMARY KEY,
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    recipient_account_id uuid REFERENCES accounts(id),
    resource_type text NOT NULL CHECK (resource_type IN ('media', 'memory', 'profile', 'agent_context')),
    resource_id text NOT NULL,
    purpose text NOT NULL,
    actions text[] NOT NULL CHECK (cardinality(actions) > 0),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    expires_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS consent_grants_owner_active
    ON consent_grants (owner_account_id, resource_type, resource_id)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS cities (
    id text PRIMARY KEY,
    name text NOT NULL,
    region text NOT NULL,
    country_code char(2) NOT NULL,
    time_zone text NOT NULL,
    content_status text NOT NULL DEFAULT 'building'
        CHECK (content_status IN ('building', 'maintained', 'review_needed')),
    publication_status text NOT NULL DEFAULT 'draft'
        CHECK (publication_status IN ('draft', 'published', 'hidden')),
    source_label text NOT NULL,
    source_ref text NOT NULL,
    maintainer_label text NOT NULL,
    maintainer_account_id uuid REFERENCES accounts(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    verified_at timestamptz,
    expires_at timestamptz
);

CREATE TABLE IF NOT EXISTS places (
    id uuid PRIMARY KEY,
    city_id text NOT NULL REFERENCES cities(id),
    name text NOT NULL,
    category_code text NOT NULL,
    summary text NOT NULL DEFAULT '',
    latitude double precision,
    longitude double precision,
    coordinate_system text NOT NULL DEFAULT 'wgs84' CHECK (coordinate_system = 'wgs84'),
    location_precision text NOT NULL DEFAULT 'none'
        CHECK (location_precision IN ('none', 'city', 'area', 'point')),
    publication_status text NOT NULL DEFAULT 'draft'
        CHECK (publication_status IN ('draft', 'published', 'hidden')),
    source_label text NOT NULL,
    source_ref text NOT NULL,
    maintainer_label text NOT NULL,
    maintainer_account_id uuid REFERENCES accounts(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    verified_at timestamptz,
    expires_at timestamptz,
    CONSTRAINT places_coordinate_pair CHECK (
        (latitude IS NULL AND longitude IS NULL) OR
        (latitude IS NOT NULL AND longitude IS NOT NULL)
    ),
    CONSTRAINT places_latitude_range CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CONSTRAINT places_longitude_range CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    CONSTRAINT places_point_coordinate CHECK (location_precision <> 'point' OR latitude IS NOT NULL),
    CONSTRAINT places_hidden_coordinate CHECK (location_precision <> 'none' OR latitude IS NULL)
);
CREATE INDEX IF NOT EXISTS places_public_by_city
    ON places (city_id, name, id) WHERE publication_status = 'published';

CREATE TABLE IF NOT EXISTS place_aliases (
    place_id uuid NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    locale text NOT NULL,
    alias text NOT NULL,
    source_ref text NOT NULL,
    PRIMARY KEY (place_id, locale, alias)
);

CREATE TABLE IF NOT EXISTS place_external_refs (
    place_id uuid NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    provider_code text NOT NULL,
    provider_place_id text NOT NULL,
    source_url text NOT NULL,
    attribution text NOT NULL,
    rights_basis text NOT NULL,
    retrieved_at timestamptz NOT NULL,
    PRIMARY KEY (provider_code, provider_place_id)
);

-- Original media stays private. Published objects refer only to approved, scrubbed variants.
CREATE TABLE IF NOT EXISTS media_assets (
    id uuid PRIMARY KEY,
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    storage_key text NOT NULL UNIQUE,
    mime_type text NOT NULL,
    byte_size bigint NOT NULL CHECK (byte_size >= 0),
    sha256_hex char(64) NOT NULL,
    state text NOT NULL DEFAULT 'quarantine'
        CHECK (state IN ('quarantine', 'scanning', 'ready_private', 'rejected', 'deleted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX IF NOT EXISTS media_assets_owner_state ON media_assets (owner_account_id, state);

CREATE TABLE IF NOT EXISTS private_media_metadata (
    media_id uuid PRIMARY KEY REFERENCES media_assets(id) ON DELETE CASCADE,
    captured_at timestamptz,
    encrypted_raw_exif bytea,
    encrypted_location bytea,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS media_variants (
    id uuid PRIMARY KEY,
    media_id uuid NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
    storage_key text NOT NULL UNIQUE,
    access_scope text NOT NULL CHECK (access_scope IN ('private', 'public')),
    metadata_stripped boolean NOT NULL DEFAULT false,
    approval_ref text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT public_variant_needs_approval CHECK (
        access_scope <> 'public' OR (metadata_stripped AND approval_ref IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_account_id uuid REFERENCES accounts(id),
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    decision text NOT NULL CHECK (decision IN ('allowed', 'denied', 'error')),
    purpose text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

-- This is only the internally selected pilot city, not a claim of live city content.
INSERT INTO cities (
    id, name, region, country_code, time_zone, content_status,
    publication_status, source_label, source_ref, maintainer_label
) VALUES (
    'aberdeen-gb', 'Aberdeen', 'Scotland', 'GB', 'Europe/London', 'building',
    'published', 'Birdtie pilot configuration', 'docs/product/01-Birdtie-产品文档-V3.0.md', 'Birdtie'
) ON CONFLICT (id) DO NOTHING;
