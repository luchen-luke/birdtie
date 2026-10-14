-- Human RSVP causal outcomes and permanent owner/operation fences only.
-- An opaque close does not require source visibility or mutate an RSVP.
BEGIN;
CREATE TABLE human_participation_operation_receipts (
 owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 operation_id uuid NOT NULL CHECK(operation_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 activity_id uuid NOT NULL CHECK(activity_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 operation text NOT NULL CHECK(operation IN('JOIN','CANCEL_RSVP')),
 effect_key text NOT NULL CHECK(effect_key ~ '^[0-9a-f]{64}$'),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 tool_version text NOT NULL CHECK(tool_version='human.participation.write.v1'),
 status text NOT NULL CHECK(status IN('COMMITTED','NO_EFFECT')),
 recorded_at timestamptz NOT NULL CHECK(recorded_at>='2000-01-01Z'::timestamptz AND recorded_at<'2201-01-01Z'::timestamptz),
 effect_id uuid REFERENCES activity_participations(id) ON DELETE RESTRICT,
 state text,
 effect_at timestamptz,
 reason text,
 PRIMARY KEY(owner_account_id,operation_id),
 CHECK(((status='COMMITTED' AND reason IS NULL AND effect_id IS NOT NULL AND effect_at IS NOT NULL AND effect_at<=recorded_at
 AND state=CASE operation WHEN 'JOIN' THEN 'going' ELSE 'cancelled' END)
 OR(status='NO_EFFECT' AND reason='USER_CLOSED' AND effect_id IS NULL AND state IS NULL AND effect_at IS NULL)) IS TRUE)
);
CREATE FUNCTION birdtie_human_participation_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p activity_participations%ROWTYPE; xid_text text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'human RSVP operation fence is immutable' USING ERRCODE='55000';END IF;
 -- Same address as every typed original writer, independent of target/action.
 PERFORM pg_advisory_xact_lock(hashtextextended('birdtie.human-rsvp-operation.v1:'||NEW.owner_account_id::text||':'||NEW.operation_id::text,0));
 xid_text:=(pg_current_xact_id()::text::bigint % 4294967296)::text;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.owner_account_id AND account_type='person' AND status='active')
 OR NEW.effect_key<>encode(sha256(convert_to('birdtie.human-participation-effect.v1'||chr(10)||NEW.owner_account_id::text||chr(10)||NEW.operation_id::text||chr(10)||NEW.operation_id::text||chr(10)||'ACTIVITY_PARTICIPATION','UTF8')),'hex')
 THEN RAISE EXCEPTION 'current human and stable operation address required' USING ERRCODE='23514';END IF;
 IF NEW.status='COMMITTED' THEN
  SELECT * INTO p FROM activity_participations WHERE id=NEW.effect_id;
  IF NOT FOUND OR p.participant_account_id IS DISTINCT FROM NEW.owner_account_id OR p.activity_id IS DISTINCT FROM NEW.activity_id OR p.status IS DISTINCT FROM NEW.state OR p.updated_at IS DISTINCT FROM NEW.effect_at
  OR NOT EXISTS(SELECT 1 FROM activity_participations original WHERE original.id=NEW.effect_id AND original.xmin::text=xid_text)
  OR NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.actor_account_id=NEW.owner_account_id AND a.resource_type='activity_participation'
   AND a.resource_id=NEW.effect_id::text AND a.target_resource_id=NEW.activity_id AND a.action=CASE NEW.operation WHEN 'JOIN' THEN 'join' ELSE 'cancel' END
   AND a.decision='allowed' AND a.purpose='human_rsvp' AND a.xmin::text=xid_text)
  THEN RAISE EXCEPTION 'same transaction original RSVP transition and audit required' USING ERRCODE='23514';END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.actor_account_id=NEW.owner_account_id AND a.resource_type='activity_participation_operation'
   AND a.resource_id=NEW.operation_id::text AND a.target_resource_id=NEW.activity_id AND a.action='close'
   AND a.decision='allowed' AND a.purpose='human_rsvp_operation_close' AND a.xmin::text=xid_text)
  THEN RAISE EXCEPTION 'same transaction explicit owner close required' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER human_participation_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON human_participation_operation_receipts FOR EACH ROW EXECUTE FUNCTION birdtie_human_participation_operation_guard();
CREATE TRIGGER human_participation_operation_truncate_guard BEFORE TRUNCATE ON human_participation_operation_receipts FOR EACH STATEMENT EXECUTE FUNCTION birdtie_human_participation_operation_guard();
COMMIT;
