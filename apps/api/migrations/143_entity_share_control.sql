BEGIN;
CREATE TABLE human_entity_share_operations(
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,
 recipient_type text NOT NULL CHECK(recipient_type IN('CONVERSATION','FRIEND_TIE')),recipient_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 conversation_id uuid,message_id uuid,entity_type text,entity_id uuid,payload_sha256 bytea,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='STOPPED' AND conversation_id IS NULL AND message_id IS NULL AND entity_type IS NULL AND entity_id IS NULL AND payload_sha256 IS NULL)OR(status='SUCCEEDED' AND conversation_id IS NOT NULL AND message_id IS NOT NULL AND entity_type IS NOT NULL AND entity_type IN('activity','place','person','community','organization','business','moment') AND entity_id IS NOT NULL AND payload_sha256 IS NOT NULL AND octet_length(payload_sha256)=32))
);
CREATE FUNCTION birdtie_entity_share_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'entity share history immutable' USING ERRCODE='23514';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':ENTITY_SHARE:'||NEW.operation_id::text,143));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'share actor unavailable' USING ERRCODE='23514';END IF;
 IF NEW.status='STOPPED' THEN
  IF EXISTS(SELECT 1 FROM conversation_messages WHERE sender_account_id=NEW.actor_account_id AND client_operation_id=NEW.operation_id) THEN RAISE EXCEPTION 'existing message cannot be stopped' USING ERRCODE='23514';END IF;RETURN NEW;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM conversation_messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=NEW.message_id AND m.conversation_id=NEW.conversation_id AND m.sender_account_id=NEW.actor_account_id AND m.client_operation_id=NEW.operation_id AND m.entity_type=NEW.entity_type AND m.entity_id=NEW.entity_id AND m.body='分享了一张卡片' AND m.speaker_kind='human' AND((NEW.recipient_type='CONVERSATION' AND NEW.recipient_id=c.id)OR(NEW.recipient_type='FRIEND_TIE' AND EXISTS(SELECT 1 FROM person_ties t WHERE t.id=NEW.recipient_id AND t.status='active'  AND t.person_a_account_id=LEAST(c.member_a_account_id,c.member_b_account_id) AND t.person_b_account_id=GREATEST(c.member_a_account_id,c.member_b_account_id))))) THEN RAISE EXCEPTION 'share receipt requires exact native effect' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER entity_share_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON human_entity_share_operations FOR EACH ROW EXECUTE FUNCTION birdtie_entity_share_operation_guard();
-- Serializes old in-flight 074 producers with the new zero-effect tombstone.
-- Text/audio/image operations retain their separate existing contracts.
CREATE FUNCTION birdtie_entity_share_late_message_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.client_operation_id IS NULL OR NEW.entity_type IS NULL OR NEW.entity_id IS NULL THEN RETURN NEW;END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.sender_account_id::text||':ENTITY_SHARE:'||NEW.client_operation_id::text,143));
 IF EXISTS(SELECT 1 FROM human_entity_share_operations WHERE actor_account_id=NEW.sender_account_id AND operation_id=NEW.client_operation_id) THEN RAISE EXCEPTION 'share original operation finalized' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER entity_share_late_message_guard BEFORE INSERT ON conversation_messages FOR EACH ROW EXECUTE FUNCTION birdtie_entity_share_late_message_guard();
COMMIT;
