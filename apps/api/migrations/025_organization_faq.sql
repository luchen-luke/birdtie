BEGIN;
CREATE TABLE IF NOT EXISTS organization_faqs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    question text NOT NULL CHECK (length(trim(question)) BETWEEN 2 AND 300),
    answer text NOT NULL CHECK (length(trim(answer)) BETWEEN 1 AND 3000),
    published boolean NOT NULL DEFAULT false,
    created_by_account_id uuid NOT NULL REFERENCES accounts(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, question)
);
CREATE INDEX IF NOT EXISTS organization_faqs_public
    ON organization_faqs (organization_id, id) WHERE published;
COMMIT;
