BEGIN;
-- Ordinary private drafts only. The immutable selector/digest ledger carries
-- no Moment text, external label, location, image bytes, or approval token.
CREATE TABLE human_moment_control_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 action text,
 moment_id uuid REFERENCES moments(id),
 expected_revision bigint,
 applied_revision bigint NOT NULL,
 payload_sha256 bytea,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK(isfinite(committed_at)),
 CHECK((status='STOPPED' AND action IS NULL AND moment_id IS NULL AND expected_revision IS NULL AND applied_revision=0 AND payload_sha256 IS NULL)
 OR(status='SUCCEEDED' AND action IN('CREATE','UPDATE','WITHDRAW') AND moment_id IS NOT NULL AND expected_revision IS NOT NULL AND expected_revision BETWEEN 0 AND 8999999999999999 AND applied_revision BETWEEN 1 AND 8999999999999999 AND payload_sha256 IS NOT NULL AND octet_length(payload_sha256)=32
 AND ((action='CREATE' AND expected_revision=0 AND applied_revision=1) OR(action='UPDATE' AND expected_revision BETWEEN 1 AND 8999999999999998 AND applied_revision=expected_revision+1) OR(action='WITHDRAW' AND expected_revision>=1 AND applied_revision=least(expected_revision+1,8999999999999999)))))
);
CREATE FUNCTION birdtie_guard_human_moment_control() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable Moment operation';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':MOMENT:'||NEW.operation_id::text,144));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'current Person required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM moments m WHERE m.id=NEW.moment_id AND m.author_account_id=NEW.actor_account_id AND m.revision=NEW.applied_revision AND ((NEW.action IN('CREATE','UPDATE') AND m.status='draft' AND m.visibility='private') OR(NEW.action='WITHDRAW' AND m.status='withdrawn' AND m.visibility='private'))) THEN RAISE EXCEPTION 'canonical owned Moment effect required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER human_moment_control_immutable BEFORE INSERT OR UPDATE OR DELETE ON human_moment_control_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_human_moment_control();
COMMIT;
