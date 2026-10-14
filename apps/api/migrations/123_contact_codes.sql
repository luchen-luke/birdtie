BEGIN;
CREATE TABLE person_contact_codes (
 owner_account_id uuid PRIMARY KEY REFERENCES accounts(id),
 revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
 code_id uuid UNIQUE,
 expires_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK ((code_id IS NULL) = (expires_at IS NULL))
);
CREATE TABLE person_contact_code_operations (
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 expected_revision bigint NOT NULL CHECK (expected_revision >= 0),
 revoke boolean NOT NULL,
 resulting_revision bigint NOT NULL CHECK (resulting_revision = expected_revision + 1),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_account_id, operation_id)
);
COMMIT;
