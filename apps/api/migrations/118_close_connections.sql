BEGIN;

-- Explicit direction: selecting a Friend for one's own CLOSE resources never
-- grants another Agent, Memory access, a capability or reciprocal closeness.
CREATE TABLE person_close_connections (
 owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 person_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 tie_id uuid NOT NULL REFERENCES person_ties(id) ON DELETE CASCADE,
 request_id uuid NOT NULL REFERENCES connection_requests(id) ON DELETE CASCADE,
 is_active boolean NOT NULL DEFAULT false,
 native_revision bigint NOT NULL DEFAULT 1 CHECK(native_revision>0),
 selected_at timestamptz,
 retired_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_account_id,person_account_id),
 CHECK(owner_account_id<>person_account_id),
 CHECK((is_active AND selected_at IS NOT NULL AND retired_at IS NULL) OR
       (NOT is_active AND retired_at IS NOT NULL))
);
CREATE INDEX person_close_connections_current_source ON person_close_connections(tie_id,request_id) WHERE is_active;

CREATE FUNCTION birdtie_connection_visible(owner uuid,viewer uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT owner IS NOT NULL AND viewer IS NOT NULL AND owner<>viewer AND EXISTS(
 SELECT 1 FROM person_ties t JOIN connection_requests r ON r.id=t.request_id
 JOIN accounts a ON a.id=owner AND a.account_type='person' AND a.status='active'
 JOIN accounts b ON b.id=viewer AND b.account_type='person' AND b.status='active'
 WHERE t.person_a_account_id=LEAST(owner,viewer) AND t.person_b_account_id=GREATEST(owner,viewer)
 AND t.status='active' AND r.scope='friend' AND r.state='accepted'
 AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id
 AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id
 AND NOT EXISTS(SELECT 1 FROM account_blocks x WHERE
 (x.blocker_account_id=owner AND x.blocked_account_id=viewer) OR
 (x.blocker_account_id=viewer AND x.blocked_account_id=owner)))
$$;
CREATE FUNCTION birdtie_close_connection_visible(owner uuid,viewer uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT birdtie_connection_visible(owner,viewer) AND EXISTS(
 SELECT 1 FROM person_close_connections c JOIN person_ties t ON t.id=c.tie_id AND t.request_id=c.request_id
 WHERE c.owner_account_id=owner AND c.person_account_id=viewer AND c.is_active
 AND t.status='active' AND t.person_a_account_id=LEAST(owner,viewer) AND t.person_b_account_id=GREATEST(owner,viewer))
$$;

CREATE FUNCTION birdtie_guard_close_connection() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n timestamptz:=clock_timestamp();
BEGIN
 IF TG_OP='DELETE' THEN
  -- Only actual parent deletion may erase a source fence. Human revocation is
  -- an UPDATE; native fixture cleanup and FK cascades remain compatible.
  IF EXISTS(SELECT 1 FROM accounts WHERE id=OLD.owner_account_id) AND
     EXISTS(SELECT 1 FROM accounts WHERE id=OLD.person_account_id) AND
     EXISTS(SELECT 1 FROM person_ties WHERE id=OLD.tie_id) AND
     EXISTS(SELECT 1 FROM connection_requests WHERE id=OLD.request_id) THEN
   RAISE EXCEPTION 'close source fence cannot be erased' USING ERRCODE='23514';
  END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.native_revision<>1 THEN RAISE EXCEPTION 'initial close revision required' USING ERRCODE='23514'; END IF;
  NEW.created_at:=n;
 ELSE
  IF NEW.owner_account_id<>OLD.owner_account_id OR NEW.person_account_id<>OLD.person_account_id OR NEW.created_at<>OLD.created_at OR NEW.native_revision<>OLD.native_revision THEN
   RAISE EXCEPTION 'close owner and native revision are immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.native_revision=9223372036854775807 THEN RAISE EXCEPTION 'close revision exhausted' USING ERRCODE='23514'; END IF;
  IF OLD.is_active AND NEW.is_active AND (NEW.tie_id<>OLD.tie_id OR NEW.request_id<>OLD.request_id) THEN
   RAISE EXCEPTION 'new friend source requires retired selection' USING ERRCODE='23514';
  END IF;
  NEW.native_revision:=OLD.native_revision+1;
 END IF;
 IF NEW.is_active THEN
  -- Protect an absent Block against a concurrent native INSERT. Original
  -- production writers acquire this lock before touching the Close row.
  LOCK TABLE account_blocks IN SHARE MODE;
  PERFORM id FROM person_ties WHERE id=NEW.tie_id FOR SHARE;
  IF NOT birdtie_connection_visible(NEW.owner_account_id,NEW.person_account_id) OR NOT EXISTS(
   SELECT 1 FROM person_ties t WHERE t.id=NEW.tie_id AND t.request_id=NEW.request_id AND t.status='active'
   AND t.person_a_account_id=LEAST(NEW.owner_account_id,NEW.person_account_id)
   AND t.person_b_account_id=GREATEST(NEW.owner_account_id,NEW.person_account_id)) THEN
   RAISE EXCEPTION 'close selection requires current accepted Friend source' USING ERRCODE='23514';
  END IF;
  NEW.selected_at:=clock_timestamp(); NEW.retired_at:=NULL;
 ELSE
  IF TG_OP='UPDATE' THEN NEW.selected_at:=OLD.selected_at; ELSE NEW.selected_at:=NULL; END IF;
  NEW.retired_at:=clock_timestamp();
 END IF;
 NEW.updated_at:=clock_timestamp(); RETURN NEW;
END $$;
CREATE TRIGGER close_connection_guard BEFORE INSERT OR UPDATE OR DELETE ON person_close_connections FOR EACH ROW EXECUTE FUNCTION birdtie_guard_close_connection();

CREATE FUNCTION birdtie_retire_close_pair(a uuid,b uuid,tie uuid,request uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 -- Retired-only fences also invalidate a pre-Block proposal that had never
 -- been selected. Unblock never deletes these fences or activates a Close.
 INSERT INTO person_close_connections(owner_account_id,person_account_id,tie_id,request_id,is_active,retired_at)
 VALUES(a,b,tie,request,false,clock_timestamp()),(b,a,tie,request,false,clock_timestamp())
 ON CONFLICT(owner_account_id,person_account_id) DO UPDATE SET is_active=false,
 native_revision=person_close_connections.native_revision,retired_at=clock_timestamp();
END $$;
CREATE FUNCTION birdtie_retire_close_tie_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='active' AND (NEW.status<>'active' OR NEW.request_id<>OLD.request_id OR NEW.person_a_account_id<>OLD.person_a_account_id OR NEW.person_b_account_id<>OLD.person_b_account_id) THEN
  PERFORM birdtie_retire_close_pair(OLD.person_a_account_id,OLD.person_b_account_id,OLD.id,OLD.request_id);
 END IF; RETURN NEW;
END $$;
CREATE TRIGGER close_tie_source_retirement AFTER UPDATE OF status,request_id,person_a_account_id,person_b_account_id ON person_ties FOR EACH ROW EXECUTE FUNCTION birdtie_retire_close_tie_source();
CREATE FUNCTION birdtie_retire_close_request_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='accepted' AND OLD.scope='friend' AND (NEW.state<>'accepted' OR NEW.scope<>'friend' OR NEW.sender_account_id<>OLD.sender_account_id OR NEW.recipient_account_id<>OLD.recipient_account_id) THEN
  UPDATE person_close_connections SET is_active=false WHERE request_id=OLD.id AND is_active;
 END IF; RETURN NEW;
END $$;
CREATE TRIGGER close_request_source_retirement AFTER UPDATE OF state,scope,sender_account_id,recipient_account_id ON connection_requests FOR EACH ROW EXECUTE FUNCTION birdtie_retire_close_request_source();
CREATE FUNCTION birdtie_retire_close_account_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='active' AND OLD.account_type='person' AND (NEW.status<>'active' OR NEW.account_type<>'person') THEN
  UPDATE person_close_connections SET is_active=false WHERE is_active AND (owner_account_id=OLD.id OR person_account_id=OLD.id);
 END IF; RETURN NEW;
END $$;
CREATE TRIGGER close_account_source_retirement AFTER UPDATE OF status,account_type ON accounts FOR EACH ROW EXECUTE FUNCTION birdtie_retire_close_account_source();
CREATE FUNCTION birdtie_retire_close_block_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE t record;
BEGIN
 FOR t IN SELECT x.id,x.request_id,x.person_a_account_id,x.person_b_account_id FROM person_ties x
  JOIN connection_requests r ON r.id=x.request_id AND r.scope='friend' AND r.state='accepted'
  WHERE x.person_a_account_id=LEAST(NEW.blocker_account_id,NEW.blocked_account_id)
    AND x.person_b_account_id=GREATEST(NEW.blocker_account_id,NEW.blocked_account_id)
 LOOP PERFORM birdtie_retire_close_pair(t.person_a_account_id,t.person_b_account_id,t.id,t.request_id); END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER close_block_source_retirement AFTER INSERT ON account_blocks FOR EACH ROW EXECUTE FUNCTION birdtie_retire_close_block_source();
COMMIT;
