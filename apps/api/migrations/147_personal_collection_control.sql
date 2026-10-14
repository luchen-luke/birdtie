-- UPDATE/WITHDRAW only. No rewriting of 124 creation, append, publication or copy.
CREATE TABLE personal_collection_control_operations (
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 action text,
 kind text,
 target_id uuid,
 expected_revision bigint,
 applied_revision bigint NOT NULL DEFAULT 0,
 payload_digest text,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(committed_at)),
 PRIMARY KEY(owner_account_id,operation_id),
 CHECK((status='STOPPED' AND action IS NULL AND kind IS NULL AND target_id IS NULL AND expected_revision IS NULL AND applied_revision=0 AND payload_digest IS NULL)
  OR(status='SUCCEEDED' AND action IS NOT NULL AND action IN('UPDATE','WITHDRAW') AND kind IS NOT NULL AND kind IN('journey','theme_map') AND target_id IS NOT NULL
   AND expected_revision IS NOT NULL AND expected_revision BETWEEN 1 AND 9000000000000000
   AND payload_digest IS NOT NULL AND payload_digest ~ '^[0-9a-f]{64}$'
   AND((action='UPDATE' AND expected_revision<9000000000000000 AND applied_revision=expected_revision+1)
    OR(action='WITHDRAW' AND applied_revision=least(expected_revision+1,9000000000000000)))))
);
CREATE FUNCTION birdtie_guard_collection_control_operation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual_owner uuid;actual_revision bigint;actual_visibility text;actual_status text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'personal collection operation is immutable' USING ERRCODE='23514';END IF;
 -- Bodyless self STOP is a zero-effect namespace fence; never queries a target.
 IF NEW.status='STOPPED' THEN RETURN NEW;END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.owner_account_id AND account_type='person' AND status='active') THEN
  RAISE EXCEPTION 'collection control requires current person' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='journey' THEN
  SELECT owner_account_id,revision,visibility,status INTO actual_owner,actual_revision,actual_visibility,actual_status FROM journeys WHERE id=NEW.target_id FOR KEY SHARE;
 ELSE
  SELECT owner_account_id,revision,visibility,status INTO actual_owner,actual_revision,actual_visibility,actual_status FROM theme_maps WHERE id=NEW.target_id FOR KEY SHARE;
 END IF;
 IF actual_owner IS DISTINCT FROM NEW.owner_account_id OR actual_revision IS DISTINCT FROM NEW.applied_revision OR actual_visibility IS DISTINCT FROM 'private'
  OR(NEW.action='UPDATE' AND actual_status IS DISTINCT FROM 'draft') OR(NEW.action='WITHDRAW' AND actual_status IS DISTINCT FROM 'withdrawn') THEN
  RAISE EXCEPTION 'collection receipt requires actual owned operation effect' USING ERRCODE='23514';
 END IF;
 -- This check applies at first insert only. Later edits/withdrawal or source loss
 -- cannot rewrite/rebind the immutable original fact.
 RETURN NEW;
END;
$$;
CREATE TRIGGER personal_collection_control_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON personal_collection_control_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_collection_control_operation();
