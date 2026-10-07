BEGIN;
CREATE TABLE organization_map_locations (
    organization_id uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    city_id text NOT NULL REFERENCES cities(id),
    latitude double precision NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude double precision NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    coordinate_system text NOT NULL DEFAULT 'wgs84' CHECK (coordinate_system = 'wgs84'),
    precision text NOT NULL DEFAULT 'point' CHECK (precision = 'point'),
    visibility text NOT NULL DEFAULT 'hidden' CHECK (visibility IN ('hidden','public')),
    review_status text NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending','approved','rejected')),
    submitted_by uuid NOT NULL REFERENCES accounts(id),
    submitted_at timestamptz NOT NULL DEFAULT now(),
    reviewed_by uuid REFERENCES accounts(id),
    reviewed_at timestamptz,
    review_note text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    CHECK ((review_status = 'pending' AND reviewed_by IS NULL AND reviewed_at IS NULL)
        OR (review_status <> 'pending' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL))
);
CREATE INDEX organization_map_locations_public ON organization_map_locations (city_id, organization_id)
    WHERE visibility='public' AND review_status='approved';
CREATE TABLE organization_map_location_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_account_id uuid NOT NULL REFERENCES accounts(id),
    action text NOT NULL CHECK (action IN ('submit','hide','approve','reject')),
    revision bigint NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
COMMIT;
