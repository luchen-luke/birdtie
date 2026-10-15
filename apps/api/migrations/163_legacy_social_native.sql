BEGIN;
-- Native social history, qualified by an immutable source mapping. Import never
-- calls message routing, notification creation or a human operation ledger.
CREATE TABLE legacy_social_import_runs(
 run_id uuid PRIMARY KEY REFERENCES legacy_import_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-social-source-v1'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256~'^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256~'^[0-9a-f]{64}$'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[0-9a-f]{64}$'),
 projection_sha256 text NOT NULL CHECK(projection_sha256~'^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(birdtie_legacy_fixed_keys(counts,ARRAY['friendships','acceptedFriends','pendingFriends','follows','conversations','members','messages','messageRequests','images']) AND birdtie_legacy_nonnegative_counts(counts)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_social_import_entities(
 id uuid PRIMARY KEY,run_id uuid NOT NULL REFERENCES legacy_social_import_runs(run_id),
 kind text NOT NULL CHECK(kind IN('friendship','follow','conversation','member','message','message_request','image')),
 source_key text NOT NULL CHECK(octet_length(source_key) BETWEEN 1 AND 100),
 source_sha256 text NOT NULL CHECK(source_sha256~'^[0-9a-f]{64}$'),
 target_id uuid NOT NULL,parent_id uuid,actor_account_id uuid REFERENCES accounts(id),other_account_id uuid REFERENCES accounts(id),
 source_state text NOT NULL DEFAULT '',source_created_at timestamptz NOT NULL,
 UNIQUE(run_id,kind,source_key)
);
CREATE TRIGGER legacy_social_run_immutable BEFORE UPDATE OR DELETE ON legacy_social_import_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_social_entity_immutable BEFORE UPDATE OR DELETE ON legacy_social_import_entities FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_social_fact(f uuid,k text,target uuid,a uuid,b uuid DEFAULT NULL,parent uuid DEFAULT NULL) RETURNS boolean
LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
 SELECT EXISTS(SELECT 1 FROM public.legacy_social_import_entities e JOIN public.legacy_social_import_runs r ON r.run_id=e.run_id
 WHERE e.id=f AND e.kind=k AND e.target_id=target AND e.actor_account_id IS NOT DISTINCT FROM a
 AND e.other_account_id IS NOT DISTINCT FROM b AND e.parent_id IS NOT DISTINCT FROM parent)
$$;
ALTER TABLE connection_requests ADD COLUMN legacy_source_id uuid REFERENCES legacy_social_import_entities(id);
ALTER TABLE connection_requests ALTER COLUMN expires_at DROP NOT NULL;
ALTER TABLE connection_requests DROP CONSTRAINT connection_requests_note_check;
ALTER TABLE connection_requests DROP CONSTRAINT connection_request_scope_city_shape;
ALTER TABLE connection_requests ADD CONSTRAINT connection_request_native_shape CHECK(
 (legacy_source_id IS NULL AND length(note) BETWEEN 1 AND 280 AND expires_at IS NOT NULL AND
 ((scope='conversation' AND city_id IS NOT NULL) OR(scope='friend' AND city_id IS NULL))) OR
 (legacy_source_id IS NOT NULL AND note='' AND expires_at IS NULL AND city_id IS NULL));
ALTER TABLE follows ADD COLUMN legacy_source_id uuid REFERENCES legacy_social_import_entities(id);
ALTER TABLE conversations ADD COLUMN legacy_source_id uuid REFERENCES legacy_social_import_entities(id);
ALTER TABLE conversations ADD COLUMN kind text NOT NULL DEFAULT 'DIRECT' CHECK(kind IN('DIRECT','PERSONAL_GROUP'));
ALTER TABLE conversations ADD COLUMN legacy_status text;
ALTER TABLE conversations ADD COLUMN legacy_title text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN legacy_join_policy text;
ALTER TABLE conversations ADD COLUMN legacy_history_policy boolean;
ALTER TABLE conversations ADD COLUMN legacy_consent_epoch bigint;
ALTER TABLE conversations ADD COLUMN legacy_last_sequence bigint NOT NULL DEFAULT 0 CHECK(legacy_last_sequence>=0);
ALTER TABLE conversations ADD COLUMN legacy_updated_at timestamptz;
ALTER TABLE conversations ADD COLUMN legacy_archived_at timestamptz;
ALTER TABLE conversations ADD COLUMN legacy_owner_id uuid REFERENCES accounts(id);
ALTER TABLE conversations ADD COLUMN legacy_journey_id uuid;
ALTER TABLE conversations ADD COLUMN legacy_school_label text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN legacy_member_limit integer;
ALTER TABLE conversations ADD COLUMN legacy_show_on_profile boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ALTER COLUMN request_id DROP NOT NULL;
ALTER TABLE conversations ALTER COLUMN member_a_account_id DROP NOT NULL;
ALTER TABLE conversations ALTER COLUMN member_b_account_id DROP NOT NULL;
ALTER TABLE conversations ADD CONSTRAINT conversation_origin_shape CHECK(
 (legacy_source_id IS NULL AND kind='DIRECT' AND request_id IS NOT NULL AND member_a_account_id IS NOT NULL AND member_b_account_id IS NOT NULL) OR
 (legacy_source_id IS NOT NULL AND ((kind='DIRECT' AND member_a_account_id IS NOT NULL AND member_b_account_id IS NOT NULL) OR
 (kind='PERSONAL_GROUP' AND member_a_account_id IS NULL AND member_b_account_id IS NULL AND request_id IS NULL))));
ALTER TABLE conversation_member_states ADD COLUMN legacy_source_id uuid REFERENCES legacy_social_import_entities(id);
ALTER TABLE conversation_member_states ADD COLUMN legacy_joined_at timestamptz;
ALTER TABLE conversation_member_states ADD COLUMN legacy_removed_at timestamptz;
ALTER TABLE conversation_member_states ADD COLUMN legacy_updated_at timestamptz;
ALTER TABLE conversation_member_states ADD COLUMN legacy_read_sequence bigint NOT NULL DEFAULT 0 CHECK(legacy_read_sequence>=0);
ALTER TABLE conversation_member_states ADD COLUMN legacy_delivered_sequence bigint NOT NULL DEFAULT 0 CHECK(legacy_delivered_sequence>=0);
ALTER TABLE conversation_member_states ADD COLUMN legacy_clear_sequence bigint NOT NULL DEFAULT 0 CHECK(legacy_clear_sequence>=0);
ALTER TABLE conversation_member_states ADD COLUMN legacy_unread_snapshot integer NOT NULL DEFAULT 0 CHECK(legacy_unread_snapshot>=0);
ALTER TABLE conversation_member_states ADD COLUMN legacy_muted_until timestamptz;
ALTER TABLE conversation_member_states ADD COLUMN legacy_hidden boolean NOT NULL DEFAULT false;
ALTER TABLE conversation_member_states ADD COLUMN legacy_pinned boolean NOT NULL DEFAULT false;
ALTER TABLE conversation_member_states ADD COLUMN legacy_marked_unread boolean NOT NULL DEFAULT false;
ALTER TABLE conversation_member_states ADD COLUMN legacy_role text;
ALTER TABLE conversation_messages ADD COLUMN legacy_source_id uuid REFERENCES legacy_social_import_entities(id);
ALTER TABLE conversation_messages ADD COLUMN message_kind text NOT NULL DEFAULT 'TEXT' CHECK(message_kind IN('TEXT','IMAGE','ENTITY','SYSTEM','STICKER'));
ALTER TABLE conversation_messages ADD COLUMN legacy_sequence bigint;
ALTER TABLE conversation_messages ADD COLUMN legacy_entity_kind text;
ALTER TABLE conversation_messages ADD COLUMN legacy_context_kind text;
ALTER TABLE conversation_messages ADD COLUMN legacy_context_id uuid;
ALTER TABLE conversation_messages ADD COLUMN legacy_request_source_id uuid;
ALTER TABLE conversation_messages ADD COLUMN legacy_client_message_id text;
ALTER TABLE conversation_messages ADD COLUMN legacy_payload_hash text;
ALTER TABLE conversation_messages ADD COLUMN legacy_sticker_pack_id text;
ALTER TABLE conversation_messages ADD COLUMN legacy_sticker_id text;
ALTER TABLE conversation_messages ADD COLUMN legacy_recalled_at timestamptz;
ALTER TABLE conversation_messages ADD COLUMN legacy_moderated_at timestamptz;
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_speaker_kind_check;
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_body_check;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_message_origin_shape CHECK(
 (legacy_source_id IS NULL AND speaker_kind='human' AND length(body) BETWEEN 1 AND 2000) OR
 (legacy_source_id IS NOT NULL AND octet_length(body)<=2000 AND client_operation_id IS NULL AND
 ((message_kind='SYSTEM' AND speaker_kind='system' AND body<>'') OR
 (message_kind IN('TEXT','STICKER') AND speaker_kind='human' AND body<>'') OR
 (message_kind IN('IMAGE','ENTITY') AND speaker_kind='human'))));
CREATE UNIQUE INDEX conversation_messages_legacy_sequence ON conversation_messages(conversation_id,legacy_sequence) WHERE legacy_sequence IS NOT NULL;
ALTER TABLE human_conversation_images ADD COLUMN legacy_source_id uuid REFERENCES legacy_social_import_entities(id);
ALTER TABLE human_conversation_images ALTER COLUMN operation_id DROP NOT NULL;
ALTER TABLE human_conversation_images ADD CONSTRAINT chat_image_operation_origin CHECK((legacy_source_id IS NULL AND operation_id IS NOT NULL) OR(legacy_source_id IS NOT NULL AND operation_id IS NULL));
-- Request history keeps an unknown original expiry NULL. The source row is
-- bound to this exact pair/ID; it is not an ordinary create-request bypass.
CREATE FUNCTION birdtie_legacy_social_request_shape() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
 IF NEW.legacy_source_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.legacy_social_import_entities e
 WHERE e.id=NEW.legacy_source_id AND e.kind IN('friendship','message_request') AND e.target_id=NEW.id
 AND e.actor_account_id=NEW.sender_account_id AND e.other_account_id=NEW.recipient_account_id
 AND e.source_state=NEW.state AND e.source_created_at=NEW.created_at) THEN
 RAISE EXCEPTION 'invalid original request source' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_social_request_shape BEFORE INSERT ON connection_requests FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_social_request_shape();
CREATE OR REPLACE FUNCTION birdtie_validate_person_tie() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE sender uuid; recipient uuid; request_scope text; request_state text;
BEGIN
    IF TG_OP='INSERT' AND EXISTS(SELECT 1 FROM public.connection_requests r JOIN public.legacy_social_import_entities e ON e.id=r.legacy_source_id
      WHERE r.id=NEW.request_id AND r.scope='friend' AND r.state='accepted' AND e.kind='friendship' AND e.source_state='accepted'
      AND LEAST(r.sender_account_id,r.recipient_account_id)=NEW.person_a_account_id
      AND GREATEST(r.sender_account_id,r.recipient_account_id)=NEW.person_b_account_id
      AND NEW.created_at=r.decided_at AND NEW.status='active') THEN RETURN NEW; END IF;

    IF TG_OP='UPDATE' AND NEW.status='removed' AND OLD.status='active'
       AND NEW.person_a_account_id=OLD.person_a_account_id AND NEW.person_b_account_id=OLD.person_b_account_id AND NEW.request_id=OLD.request_id
       AND EXISTS(SELECT 1 FROM public.connection_requests r WHERE r.id=OLD.request_id AND r.legacy_source_id IS NOT NULL AND r.scope='friend' AND r.state='accepted') THEN RETURN NEW; END IF;
    SELECT sender_account_id,recipient_account_id,scope,state
    INTO sender,recipient,request_scope,request_state
    FROM connection_requests WHERE id=NEW.request_id;
    IF request_scope IS DISTINCT FROM 'friend' OR request_state IS DISTINCT FROM 'accepted' OR
       LEAST(sender,recipient) IS DISTINCT FROM NEW.person_a_account_id OR
       GREATEST(sender,recipient) IS DISTINCT FROM NEW.person_b_account_id THEN
        RAISE EXCEPTION 'Tie requires accepted matching friend request';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.person_a_account_id
        AND account_type='person' AND status='active') OR
       NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.person_b_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Tie requires two active People';
    END IF;
    IF NEW.status='active' AND EXISTS (SELECT 1 FROM account_blocks b
        WHERE (b.blocker_account_id=NEW.person_a_account_id AND
               b.blocked_account_id=NEW.person_b_account_id) OR
              (b.blocker_account_id=NEW.person_b_account_id AND
               b.blocked_account_id=NEW.person_a_account_id)) THEN
        RAISE EXCEPTION 'blocked People cannot establish active Tie';
    END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_validate_follow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND NEW.legacy_source_id IS NOT NULL AND public.birdtie_legacy_social_fact(NEW.legacy_source_id,'follow',NEW.id,NEW.follower_account_id,NEW.person_account_id) THEN RETURN NEW; END IF;
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.follower_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Follow requires active Person';
    END IF;
    IF NEW.person_account_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=NEW.person_account_id
            AND a.account_type='person' AND a.status='active') OR
           NOT birdtie_profile_visible_to(NEW.person_account_id,NEW.follower_account_id) THEN
            RAISE EXCEPTION 'Person Follow target unavailable';
        END IF;
    ELSIF NEW.organization_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM organizations o JOIN accounts a ON a.id=o.account_id
            WHERE o.id=NEW.organization_id AND o.status='active' AND a.status='active') THEN
            RAISE EXCEPTION 'Organization Follow target unavailable';
        END IF;
    ELSIF NEW.community_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM communities c WHERE c.id=NEW.community_id
            AND c.lifecycle_status='active' AND c.publication_status='published'
            AND c.visibility='public') THEN
            RAISE EXCEPTION 'Community Follow target unavailable';
        END IF;
    ELSIF NEW.business_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
            WHERE b.id=NEW.business_id AND b.status='active' AND b.claim_status='verified'
            AND a.status='active') THEN
            RAISE EXCEPTION 'Business Follow target unavailable';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_validate_conversation_member_state() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.legacy_source_id IS NOT NULL AND public.birdtie_legacy_social_fact(NEW.legacy_source_id,'member',NEW.member_account_id,NEW.member_account_id,NULL,NEW.conversation_id) THEN RETURN NEW; END IF;

    IF NOT EXISTS (SELECT 1 FROM conversations cv WHERE cv.id=NEW.conversation_id
        AND NEW.member_account_id IN(cv.member_a_account_id,cv.member_b_account_id)) THEN
        RAISE EXCEPTION 'read state requires conversation member';
    END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_create_conversation_member_states() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.legacy_source_id IS NOT NULL THEN RETURN NEW; END IF;

    INSERT INTO conversation_member_states(conversation_id,member_account_id,baseline_at)
    VALUES(NEW.id,NEW.member_a_account_id,NEW.created_at),
          (NEW.id,NEW.member_b_account_id,NEW.created_at);
    RETURN NEW;
END $$;

CREATE FUNCTION birdtie_legacy_social_parent_shape() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
 IF NEW.legacy_source_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.legacy_social_import_entities e
 WHERE e.id=NEW.legacy_source_id AND e.kind='conversation' AND e.target_id=NEW.id
 AND e.actor_account_id IS NOT DISTINCT FROM NEW.member_a_account_id AND e.other_account_id IS NOT DISTINCT FROM NEW.member_b_account_id) THEN
 RAISE EXCEPTION 'invalid original conversation' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_social_parent_shape BEFORE INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_social_parent_shape();
CREATE FUNCTION birdtie_legacy_social_message_shape() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE cv public.conversations%ROWTYPE;
BEGIN
 SELECT * INTO cv FROM public.conversations WHERE id=NEW.conversation_id FOR UPDATE;
 IF NEW.legacy_source_id IS NOT NULL THEN
  IF cv.legacy_source_id IS NULL OR NOT public.birdtie_legacy_social_fact(NEW.legacy_source_id,'message',NEW.id,NEW.sender_account_id,NULL,NEW.conversation_id)
  OR NEW.legacy_sequence IS NULL OR NEW.legacy_sequence<1 OR NEW.legacy_sequence>cv.legacy_last_sequence
  OR NOT EXISTS(SELECT 1 FROM public.conversation_member_states WHERE conversation_id=NEW.conversation_id AND member_account_id=NEW.sender_account_id) THEN
   RAISE EXCEPTION 'invalid original message' USING ERRCODE='23514'; END IF;
 ELSIF cv.legacy_source_id IS NOT NULL THEN
  IF cv.kind<>'DIRECT' OR cv.request_id IS NULL THEN RAISE EXCEPTION 'historical conversation read only' USING ERRCODE='23514'; END IF;
  UPDATE public.conversations SET legacy_last_sequence=legacy_last_sequence+1 WHERE id=cv.id RETURNING legacy_last_sequence INTO NEW.legacy_sequence;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_social_message_shape BEFORE INSERT ON conversation_messages FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_social_message_shape();
CREATE OR REPLACE FUNCTION birdtie_private_chat_image_shape() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE original public.conversation_messages%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN
  IF EXISTS(SELECT 1 FROM public.conversation_messages WHERE id=OLD.message_id) THEN
   RAISE EXCEPTION 'cannot orphan image from original message' USING ERRCODE='55000';
  END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'image message receipt is immutable' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
 END IF;

 IF NEW.legacy_source_id IS NOT NULL THEN
  IF NOT public.birdtie_legacy_social_fact(NEW.legacy_source_id,'image',NEW.message_id,NEW.owner_account_id,NEW.recipient_account_id,NEW.conversation_id)
    OR NEW.operation_id IS NOT NULL OR NEW.caption_present OR NEW.caption_sha256<>encode(pg_catalog.sha256(''::bytea),'hex')
    OR NOT EXISTS(SELECT 1 FROM public.conversation_messages m JOIN public.conversations cv ON cv.id=m.conversation_id
      WHERE m.id=NEW.message_id AND m.conversation_id=NEW.conversation_id AND m.sender_account_id=NEW.owner_account_id
      AND m.legacy_source_id IS NOT NULL AND m.message_kind='IMAGE' AND m.created_at=NEW.created_at
      AND cv.kind='DIRECT' AND NEW.recipient_account_id=CASE WHEN cv.member_a_account_id=NEW.owner_account_id THEN cv.member_b_account_id ELSE cv.member_a_account_id END)
    OR EXISTS(SELECT 1 FROM public.human_conversation_audio a WHERE a.message_id=NEW.message_id) THEN
    RAISE EXCEPTION 'invalid original private image' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO original FROM public.conversation_messages WHERE id=NEW.message_id FOR UPDATE;
 IF NOT FOUND OR original.sender_account_id<>NEW.owner_account_id
  OR original.conversation_id<>NEW.conversation_id
  OR original.client_operation_id IS DISTINCT FROM NEW.operation_id
  OR original.speaker_kind<>'human'
  OR original.entity_type IS NOT NULL OR original.entity_id IS NOT NULL
  OR original.created_at<>NEW.created_at
  OR octet_length(original.body) NOT BETWEEN 1 AND 2000
  OR (NOT NEW.caption_present AND original.body<>'图片消息')
  OR NEW.caption_sha256<>encode(pg_catalog.sha256(convert_to(
     CASE WHEN NEW.caption_present THEN original.body ELSE '' END,'UTF8')),'hex')
  OR NOT EXISTS(SELECT 1 FROM public.conversations c WHERE c.id=NEW.conversation_id
    AND NEW.owner_account_id IN (c.member_a_account_id,c.member_b_account_id)
    AND NEW.recipient_account_id=CASE WHEN c.member_a_account_id=NEW.owner_account_id
      THEN c.member_b_account_id ELSE c.member_a_account_id END)
  OR EXISTS(SELECT 1 FROM public.human_conversation_audio a WHERE a.message_id=NEW.message_id)
 THEN
  RAISE EXCEPTION 'image must bind its original exclusive human message' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Original membership authorizes private history only. It does not expose a
-- peer profile or grant send permission. Block retires the entire group read.
CREATE FUNCTION birdtie_legacy_social_member_visible(cv_id uuid,viewer uuid) RETURNS boolean
LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
 SELECT EXISTS(SELECT 1 FROM public.conversations cv
 JOIN public.conversation_member_states cms ON cms.conversation_id=cv.id AND cms.member_account_id=viewer
 JOIN public.accounts own ON own.id=viewer AND own.account_type='person' AND own.status='active'
 JOIN public.legacy_social_import_entities e ON e.id=cv.legacy_source_id AND e.kind='conversation' AND e.target_id=cv.id
 WHERE cv.id=cv_id AND cv.legacy_archived_at IS NULL AND cms.legacy_removed_at IS NULL AND NOT cms.legacy_hidden
 AND cms.legacy_source_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.conversation_member_states peer JOIN public.account_blocks b ON
 (b.blocker_account_id=viewer AND b.blocked_account_id=peer.member_account_id) OR(b.blocked_account_id=viewer AND b.blocker_account_id=peer.member_account_id)
 WHERE peer.conversation_id=cv.id AND peer.member_account_id<>viewer AND peer.legacy_removed_at IS NULL))
$$;
CREATE FUNCTION birdtie_legacy_social_message_visible(message uuid,viewer uuid) RETURNS boolean
LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
 SELECT EXISTS(SELECT 1 FROM public.conversation_messages m
 JOIN public.conversations cv ON cv.id=m.conversation_id
 JOIN public.conversation_member_states cms ON cms.conversation_id=cv.id AND cms.member_account_id=viewer
 WHERE m.id=message AND public.birdtie_legacy_social_member_visible(cv.id,viewer)
 AND m.legacy_sequence>cms.legacy_clear_sequence
 AND (cv.kind='DIRECT' OR cv.legacy_history_policy OR m.created_at>=cms.legacy_joined_at)
 AND (m.legacy_request_source_id IS NULL OR m.sender_account_id=viewer OR EXISTS(
 SELECT 1 FROM public.legacy_social_import_entities request WHERE request.kind='message_request' AND request.source_key=m.legacy_request_source_id::text AND request.parent_id=cv.id AND request.source_state='accepted')))
$$;
CREATE FUNCTION birdtie_legacy_social_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'legacy-social-native-v1'::text $$;
COMMIT;
