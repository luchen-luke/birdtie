-- Explicit human contact requests and one-to-one conversations. No Agent
-- output is stored as a human message and no connection is auto-accepted.
CREATE TABLE IF NOT EXISTS connection_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_account_id uuid NOT NULL REFERENCES accounts(id),
    recipient_account_id uuid NOT NULL REFERENCES accounts(id),
    city_id text NOT NULL REFERENCES cities(id),
    note text NOT NULL CHECK (length(note) BETWEEN 1 AND 280),
    scope text NOT NULL DEFAULT 'conversation' CHECK (scope = 'conversation'),
    state text NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'accepted', 'declined', 'withdrawn', 'expired')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    CONSTRAINT connection_request_distinct_accounts
        CHECK (sender_account_id <> recipient_account_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS connection_requests_pending_pair
    ON connection_requests(
        LEAST(sender_account_id, recipient_account_id),
        GREATEST(sender_account_id, recipient_account_id)
    ) WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS connection_requests_sender_recent
    ON connection_requests(sender_account_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS connection_requests_recipient_recent
    ON connection_requests(recipient_account_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id uuid NOT NULL UNIQUE REFERENCES connection_requests(id),
    member_a_account_id uuid NOT NULL REFERENCES accounts(id),
    member_b_account_id uuid NOT NULL REFERENCES accounts(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT conversation_distinct_members
        CHECK (member_a_account_id <> member_b_account_id)
);
CREATE INDEX IF NOT EXISTS conversations_member_a_recent
    ON conversations(member_a_account_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS conversations_member_b_recent
    ON conversations(member_b_account_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS conversation_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    sender_account_id uuid NOT NULL REFERENCES accounts(id),
    speaker_kind text NOT NULL DEFAULT 'human' CHECK (speaker_kind = 'human'),
    body text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS conversation_messages_recent
    ON conversation_messages(conversation_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS conversation_messages_sender_rate
    ON conversation_messages(sender_account_id, created_at DESC);

ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check
    CHECK (resource_type IN (
        'place_candidate', 'activity_candidate', 'community',
        'connection_request', 'conversation_message'
    ));
