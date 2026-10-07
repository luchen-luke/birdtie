BEGIN;

-- Existing conversations and messages already have durable IDs and timestamps.
-- Historical messages have unknown read status; baseline them without claiming they were read.
CREATE UNIQUE INDEX conversation_messages_conversation_id_id
    ON conversation_messages(conversation_id,id);

CREATE TABLE conversation_member_states (
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    member_account_id uuid NOT NULL REFERENCES accounts(id),
    baseline_at timestamptz NOT NULL DEFAULT now(),
    last_read_message_id uuid,
    last_read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(conversation_id,member_account_id),
    CONSTRAINT conversation_read_cursor_pair
        FOREIGN KEY(conversation_id,last_read_message_id)
        REFERENCES conversation_messages(conversation_id,id),
    CONSTRAINT conversation_read_cursor_shape
        CHECK ((last_read_message_id IS NULL) = (last_read_at IS NULL))
);
CREATE INDEX conversation_member_states_member
    ON conversation_member_states(member_account_id,conversation_id);

CREATE FUNCTION birdtie_validate_conversation_member_state() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM conversations cv WHERE cv.id=NEW.conversation_id
        AND NEW.member_account_id IN(cv.member_a_account_id,cv.member_b_account_id)) THEN
        RAISE EXCEPTION 'read state requires conversation member';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER conversation_member_state_validate BEFORE INSERT OR UPDATE OF
    conversation_id,member_account_id ON conversation_member_states
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_conversation_member_state();

INSERT INTO conversation_member_states(conversation_id,member_account_id,baseline_at)
SELECT id,member_a_account_id,now() FROM conversations
UNION ALL SELECT id,member_b_account_id,now() FROM conversations;

CREATE FUNCTION birdtie_create_conversation_member_states() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO conversation_member_states(conversation_id,member_account_id,baseline_at)
    VALUES(NEW.id,NEW.member_a_account_id,NEW.created_at),
          (NEW.id,NEW.member_b_account_id,NEW.created_at);
    RETURN NEW;
END $$;
CREATE TRIGGER conversation_member_states_create AFTER INSERT ON conversations
    FOR EACH ROW EXECUTE FUNCTION birdtie_create_conversation_member_states();

COMMIT;
