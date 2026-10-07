-- Causal human Request outcome only; original Request/Tie/Conversation remain
-- the sole domain authorities. No approval, session, body or executable handle.
BEGIN;
CREATE TABLE connection_request_decision_receipts (
 owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 operation_id uuid NOT NULL,
 request_id uuid NOT NULL REFERENCES connection_requests(id) ON DELETE RESTRICT,
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 action text NOT NULL CHECK(action IN('accept','decline','withdraw')),
 scope text NOT NULL CHECK(scope IN('friend','conversation')),
 status text NOT NULL CHECK(status IN('COMMITTED','NO_EFFECT')),
 state text NOT NULL,
 reason text,
 recorded_at timestamptz NOT NULL,
 PRIMARY KEY(owner_account_id,operation_id),
 CHECK((status='COMMITTED' AND reason IS NULL AND state=CASE action WHEN 'accept' THEN 'accepted' WHEN 'decline' THEN 'declined' ELSE 'withdrawn' END)
 OR(status='NO_EFFECT' AND state='' AND reason IS NOT NULL AND reason IN('EXPIRED','ALREADY_DECIDED')))
);
CREATE FUNCTION birdtie_connection_decision_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r connection_requests%ROWTYPE;
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'decision receipt is immutable' USING ERRCODE='55000';END IF;
 SELECT * INTO r FROM connection_requests WHERE id=NEW.request_id;
 IF NOT FOUND OR r.scope<>NEW.scope OR
 (NEW.action='withdraw' AND r.sender_account_id<>NEW.owner_account_id) OR
 (NEW.action<>'withdraw' AND r.recipient_account_id<>NEW.owner_account_id) OR
 NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.owner_account_id AND account_type='person' AND status='active')
 THEN RAISE EXCEPTION 'original current human request binding required' USING ERRCODE='23514';END IF;
 IF NEW.status='COMMITTED' AND (r.state<>NEW.state OR NOT EXISTS(
 SELECT 1 FROM connection_requests original WHERE original.id=NEW.request_id
 AND original.xmin::text=(pg_current_xact_id()::text::bigint % 4294967296)::text) OR NOT EXISTS(
 SELECT 1 FROM audit_events a WHERE a.actor_account_id=NEW.owner_account_id
 AND a.resource_type='connection_request' AND a.resource_id=NEW.request_id::text
 AND a.action=NEW.state AND a.decision='allowed' AND a.purpose='human_contact'
 AND a.xmin::text=(pg_current_xact_id()::text::bigint % 4294967296)::text))
 THEN RAISE EXCEPTION 'same transaction original decision required' USING ERRCODE='23514';END IF;
 IF NEW.status='NO_EFFECT' AND (NEW.reason IS NULL OR NOT (
 (NEW.reason='EXPIRED' AND r.state='pending' AND r.expires_at<=NEW.recorded_at)
 OR(NEW.reason='ALREADY_DECIDED' AND r.state<>'pending')))
 THEN RAISE EXCEPTION 'authoritative pre-effect rejection required' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER connection_decision_receipt_guard BEFORE INSERT OR UPDATE OR DELETE ON connection_request_decision_receipts FOR EACH ROW EXECUTE FUNCTION birdtie_connection_decision_receipt_guard();
COMMIT;
