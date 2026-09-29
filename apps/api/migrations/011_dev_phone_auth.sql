-- Local-only challenge state. This never claims a phone number was verified by SMS.
CREATE TABLE IF NOT EXISTS dev_phone_challenges (
    phone_digest bytea PRIMARY KEY CHECK (octet_length(phone_digest) = 32),
    requested_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT (now() + interval '5 minutes'),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5)
);
CREATE INDEX IF NOT EXISTS dev_phone_challenges_expiry
    ON dev_phone_challenges (expires_at);
