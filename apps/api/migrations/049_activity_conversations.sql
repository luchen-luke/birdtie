BEGIN;
CREATE TABLE activity_conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id uuid NOT NULL UNIQUE REFERENCES activities(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE activity_conversation_members (
    conversation_id uuid NOT NULL REFERENCES activity_conversations(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','left')),
    joined_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(conversation_id,account_id)
);
CREATE TABLE activity_conversation_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES activity_conversations(id) ON DELETE CASCADE,
    sender_account_id uuid NOT NULL REFERENCES accounts(id),
    client_message_id uuid NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    removed_at timestamptz,
    removed_by uuid REFERENCES accounts(id),
    CHECK((removed_at IS NULL)=(removed_by IS NULL)),
    CHECK((removed_at IS NULL AND char_length(body) BETWEEN 1 AND 2000) OR (removed_at IS NOT NULL AND body='')),
    FOREIGN KEY(conversation_id,sender_account_id) REFERENCES activity_conversation_members(conversation_id,account_id),
    UNIQUE(conversation_id,sender_account_id,client_message_id)
);
CREATE INDEX activity_chat_messages_recent ON activity_conversation_messages(conversation_id,created_at DESC,id DESC);
CREATE INDEX activity_chat_sender_recent ON activity_conversation_messages(sender_account_id,created_at DESC);
CREATE OR REPLACE FUNCTION birdtie_activity_chat_rsvp_revoke() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status<>'going' THEN
        UPDATE activity_conversation_members m SET status='left',updated_at=now()
        FROM activity_conversations c WHERE c.id=m.conversation_id AND c.activity_id=NEW.activity_id
            AND m.account_id=NEW.participant_account_id AND m.status='active';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER activity_chat_rsvp_revoke AFTER UPDATE OF status ON activity_participations
    FOR EACH ROW EXECUTE FUNCTION birdtie_activity_chat_rsvp_revoke();
ALTER TABLE incident_reports DROP CONSTRAINT incident_reports_target_type_check;
ALTER TABLE incident_reports ADD CONSTRAINT incident_reports_target_type_check
    CHECK(target_type IN ('activity','organization','account','message','activity_message','community','business','general'));
COMMIT;
