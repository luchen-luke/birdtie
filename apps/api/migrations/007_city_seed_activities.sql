-- A City Seed event can have an external host without a Birdtie Account.
ALTER TABLE activities ALTER COLUMN host_account_id DROP NOT NULL;
ALTER TABLE activities ADD COLUMN IF NOT EXISTS host_label text NOT NULL DEFAULT '';
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'activity_host_label_present'
    ) THEN
        ALTER TABLE activities ADD CONSTRAINT activity_host_label_present
            CHECK (length(host_label) > 0);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS activity_candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    city_id text NOT NULL REFERENCES cities(id),
    submitted_by uuid NOT NULL REFERENCES accounts(id),
    place_id uuid,
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    summary text NOT NULL DEFAULT '' CHECK (length(summary) <= 3000),
    host_label text NOT NULL CHECK (length(host_label) BETWEEN 1 AND 160),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    time_zone text NOT NULL,
    source_label text NOT NULL,
    source_url text NOT NULL,
    rights_note text NOT NULL,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'published', 'rejected')),
    reviewed_by uuid REFERENCES accounts(id),
    reviewed_at timestamptz,
    review_note text,
    resolved_activity_id uuid REFERENCES activities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (place_id, city_id) REFERENCES places(id, city_id),
    CONSTRAINT activity_candidate_time CHECK (ends_at > starts_at),
    CONSTRAINT activity_candidate_expiry CHECK (expires_at > created_at),
    CONSTRAINT activity_candidate_review_state CHECK (
        (status = 'pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND resolved_activity_id IS NULL) OR
        (status = 'rejected' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND resolved_activity_id IS NULL) OR
        (status = 'published' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND resolved_activity_id IS NOT NULL)
    )
);
CREATE INDEX IF NOT EXISTS activity_candidates_city_pending
    ON activity_candidates (city_id, created_at, id) WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS activity_sources (
    activity_id uuid NOT NULL REFERENCES activities(id),
    candidate_id uuid NOT NULL UNIQUE REFERENCES activity_candidates(id),
    source_label text NOT NULL,
    source_url text NOT NULL,
    rights_note text NOT NULL,
    reviewer_account_id uuid NOT NULL REFERENCES accounts(id),
    verified_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (activity_id, candidate_id)
);

CREATE TABLE IF NOT EXISTS city_seed_activities (
    city_id text NOT NULL REFERENCES cities(id),
    activity_id uuid NOT NULL REFERENCES activities(id),
    maintainer_account_id uuid NOT NULL REFERENCES accounts(id),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'review_needed', 'removed')),
    valid_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (city_id, activity_id)
);
