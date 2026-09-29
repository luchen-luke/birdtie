-- Single-use OIDC state and Birdtie code handoff. No provider tokens are stored.
CREATE TABLE IF NOT EXISTS oidc_login_flows (
    state_sha256 bytea PRIMARY KEY CHECK (octet_length(state_sha256) = 32),
    nonce text NOT NULL,
    provider_verifier text NOT NULL,
    client_challenge text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT (now() + interval '5 minutes'),
    CONSTRAINT oidc_login_flow_expiry CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS oidc_login_flows_expiry ON oidc_login_flows (expires_at);

CREATE TABLE IF NOT EXISTS oidc_exchange_codes (
    code_sha256 bytea PRIMARY KEY CHECK (octet_length(code_sha256) = 32),
    account_id uuid NOT NULL REFERENCES accounts(id),
    client_challenge text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT (now() + interval '1 minute'),
    CONSTRAINT oidc_exchange_code_expiry CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS oidc_exchange_codes_expiry ON oidc_exchange_codes (expires_at);
