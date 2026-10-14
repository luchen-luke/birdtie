-- Private human material only. A clean scanner and a current bound approval
-- are required before the worker can make an object ready_private.
CREATE TABLE IF NOT EXISTS private_video_materials (
    id uuid PRIMARY KEY REFERENCES media_assets(id),
    owner_account_id uuid NOT NULL REFERENCES accounts(id),
    operation_id uuid NOT NULL,
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
    byte_size bigint NOT NULL CHECK (byte_size BETWEEN 1 AND 20971520),
    duration_ms bigint NOT NULL CHECK (duration_ms BETWEEN 1 AND 30000),
    mime_type text NOT NULL CHECK (mime_type='video/mp4'),
    purpose text NOT NULL CHECK (purpose='HUMAN_PRIVATE_VIDEO_MATERIAL'),
    storage_policy_id text NOT NULL CHECK (storage_policy_id ~ '^[0-9a-f]{64}$'),
    quarantine_key text NOT NULL UNIQUE,
    private_key text NOT NULL UNIQUE,
    approval_session_id uuid NOT NULL REFERENCES sessions(id),
    approval_session_digest bytea NOT NULL CHECK (octet_length(approval_session_digest)=32),
    approval_generation bigint NOT NULL CHECK (approval_generation>0),
    status text NOT NULL DEFAULT 'quarantine' CHECK (status IN('quarantine','queued','scanning','ready_private','rejected','deleting','deleted')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision>0),
    width integer NOT NULL DEFAULT 0,
    height integer NOT NULL DEFAULT 0,
    failure_code text NOT NULL DEFAULT '',
    upload_expires_at timestamptz NOT NULL,
    retain_until timestamptz NOT NULL,
    lease_id uuid,
    lease_until timestamptz,
    cleanup_pending boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(owner_account_id,operation_id),
    CHECK (retain_until>created_at AND upload_expires_at<=retain_until),
    CHECK (status<>'ready_private' OR (width BETWEEN 1 AND 1920 AND height BETWEEN 1 AND 1920 AND width::bigint*height<=2073600)),
    CHECK ((lease_id IS NULL)=(lease_until IS NULL))
);
CREATE INDEX IF NOT EXISTS private_video_owner_page ON private_video_materials(owner_account_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS private_video_worker ON private_video_materials(status,lease_until,created_at);
