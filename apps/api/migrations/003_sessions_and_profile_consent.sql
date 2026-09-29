-- Sessions are issued only after an external identity assertion is verified.
-- Store token digests, never bearer tokens or identity-provider credentials.
CREATE TABLE IF NOT EXISTS account_auth_identities (
    issuer text NOT NULL,
    subject text NOT NULL,
    account_id uuid NOT NULL REFERENCES accounts(id),
    verified_at timestamptz NOT NULL,
    PRIMARY KEY (issuer, subject),
    CONSTRAINT account_auth_identities_nonempty CHECK (issuer <> '' AND subject <> '')
);
CREATE INDEX IF NOT EXISTS account_auth_identities_account
    ON account_auth_identities (account_id);

CREATE TABLE IF NOT EXISTS sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts(id),
    token_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    authentication_method text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CONSTRAINT sessions_expiry CHECK (expires_at > created_at),
    CONSTRAINT sessions_idle_expiry CHECK (idle_expires_at > created_at AND idle_expires_at <= expires_at)
);
CREATE INDEX IF NOT EXISTS sessions_account_active
    ON sessions (account_id, expires_at) WHERE revoked_at IS NULL;

-- Blocks deny reads by authenticated actors even if a grant or public flag exists.
CREATE TABLE IF NOT EXISTS account_blocks (
    blocker_account_id uuid NOT NULL REFERENCES accounts(id),
    blocked_account_id uuid NOT NULL REFERENCES accounts(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_account_id, blocked_account_id),
    CONSTRAINT account_blocks_not_self CHECK (blocker_account_id <> blocked_account_id)
);
CREATE INDEX IF NOT EXISTS account_blocks_blocked
    ON account_blocks (blocked_account_id, blocker_account_id);

CREATE INDEX IF NOT EXISTS consent_grants_profile_recipient
    ON consent_grants (recipient_account_id, resource_type, resource_id, purpose)
    WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS consent_grants_active_unique
    ON consent_grants (owner_account_id, recipient_account_id, resource_type, resource_id, purpose)
    WHERE revoked_at IS NULL AND recipient_account_id IS NOT NULL;
