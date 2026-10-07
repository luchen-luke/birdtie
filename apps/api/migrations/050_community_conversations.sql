BEGIN;
CREATE TABLE community_conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL UNIQUE REFERENCES communities(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE community_conversation_members (
    conversation_id uuid NOT NULL REFERENCES community_conversations(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','left')),
    joined_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(conversation_id,account_id)
);
CREATE TABLE community_conversation_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES community_conversations(id) ON DELETE CASCADE,
    sender_account_id uuid NOT NULL REFERENCES accounts(id),
    client_message_id uuid NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    removed_at timestamptz,
    removed_by uuid REFERENCES accounts(id),
    CHECK((removed_at IS NULL)=(removed_by IS NULL)),
    CHECK((removed_at IS NULL AND char_length(body) BETWEEN 1 AND 2000) OR (removed_at IS NOT NULL AND body='')),
    FOREIGN KEY(conversation_id,sender_account_id) REFERENCES community_conversation_members(conversation_id,account_id),
    UNIQUE(conversation_id,sender_account_id,client_message_id)
);
CREATE INDEX community_chat_messages_recent ON community_conversation_messages(conversation_id,created_at DESC,id DESC);
CREATE INDEX community_chat_sender_recent ON community_conversation_messages(sender_account_id,created_at DESC);
CREATE OR REPLACE FUNCTION birdtie_community_chat_membership_revoke() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_community uuid; target_person uuid;
BEGIN
    IF TG_OP='DELETE' THEN
        target_community:=OLD.community_id; target_person:=OLD.user_account_id;
    ELSIF NEW.status<>'active' THEN
        target_community:=NEW.community_id; target_person:=NEW.user_account_id;
    ELSE
        RETURN NULL;
    END IF;
    UPDATE community_conversation_members m SET status='left',updated_at=now()
    FROM community_conversations c WHERE c.id=m.conversation_id AND c.community_id=target_community
        AND m.account_id=target_person AND m.status='active';
    RETURN NULL;
END $$;
CREATE TRIGGER community_chat_membership_revoke AFTER UPDATE OF status OR DELETE ON community_memberships
    FOR EACH ROW EXECUTE FUNCTION birdtie_community_chat_membership_revoke();
ALTER TABLE incident_reports DROP CONSTRAINT incident_reports_target_type_check;
ALTER TABLE incident_reports ADD CONSTRAINT incident_reports_target_type_check
    CHECK(target_type IN ('activity','organization','account','message','activity_message','community_message','community','business','general'));
COMMIT;
