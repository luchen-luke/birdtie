-- Private create idempotency metadata only. social_intents remains the entity
-- authority; this is not an AIR effect ledger, approval or publication grant.
BEGIN;
CREATE TABLE social_intent_creation_receipts (
 owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 operation_id uuid NOT NULL,
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 source_task_id uuid,
 status text NOT NULL CHECK(status IN('COMMITTED','NO_EFFECT')),
 intent_id uuid REFERENCES social_intents(id) ON DELETE RESTRICT,
 prior_intent_id uuid REFERENCES social_intents(id) ON DELETE RESTRICT,
 reason text,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_account_id,operation_id),
 CHECK((status='COMMITTED' AND intent_id IS NOT NULL AND prior_intent_id IS NULL AND reason IS NULL)
 OR(status='NO_EFFECT' AND intent_id IS NULL AND
 ((reason IN('EXPIRED','SOURCE_UNAVAILABLE','SOURCE_CHANGED') AND prior_intent_id IS NULL)
 OR(reason='SOURCE_ALREADY_EXISTS' AND source_task_id IS NOT NULL AND prior_intent_id IS NOT NULL))))
);
CREATE FUNCTION birdtie_social_intent_creation_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'creation receipt is immutable' USING ERRCODE='55000';END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.owner_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'active personal owner required' USING ERRCODE='23514';END IF;
 IF NEW.status='COMMITTED' AND NOT EXISTS(SELECT 1 FROM social_intents WHERE id=NEW.intent_id AND creator_account_id=NEW.owner_account_id AND audience='PRIVATE' AND status='DRAFT' AND source_agent_task_id IS NOT DISTINCT FROM NEW.source_task_id) THEN RAISE EXCEPTION 'original private creation binding required' USING ERRCODE='23514';END IF;
 IF NEW.reason='SOURCE_ALREADY_EXISTS' AND NOT EXISTS(SELECT 1 FROM social_intents WHERE id=NEW.prior_intent_id AND creator_account_id=NEW.owner_account_id AND source_agent_task_id=NEW.source_task_id) THEN RAISE EXCEPTION 'original owner source binding required' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER social_intent_creation_receipt_guard BEFORE INSERT OR UPDATE ON social_intent_creation_receipts FOR EACH ROW EXECUTE FUNCTION birdtie_social_intent_creation_receipt_guard();
COMMIT;
