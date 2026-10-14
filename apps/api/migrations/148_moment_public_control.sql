BEGIN;
-- Extend the original Moment operation namespace; no payload or approval is
-- retained here. Existing 144/145 migrations and successful receipts remain.
DO $$ DECLARE item record; BEGIN
 FOR item IN SELECT conname FROM pg_constraint WHERE conrelid='human_moment_control_operations'::regclass AND contype='c' AND position('action' IN pg_get_constraintdef(oid))>0 LOOP
  EXECUTE format('ALTER TABLE human_moment_control_operations DROP CONSTRAINT %I',item.conname);
 END LOOP;
END $$;
ALTER TABLE human_moment_control_operations ADD CONSTRAINT human_moment_control_effect_shape CHECK(
 (status='STOPPED' AND action IS NULL AND moment_id IS NULL AND expected_revision IS NULL AND applied_revision=0 AND payload_sha256 IS NULL)
 OR(status='SUCCEEDED' AND action IN('CREATE','UPDATE','WITHDRAW','PUBLISH','UPDATE_PUBLIC') AND moment_id IS NOT NULL AND expected_revision IS NOT NULL AND expected_revision BETWEEN 0 AND 8999999999999999 AND applied_revision BETWEEN 1 AND 8999999999999999 AND payload_sha256 IS NOT NULL AND octet_length(payload_sha256)=32
 AND((action='CREATE' AND expected_revision=0 AND applied_revision=1) OR(action IN('UPDATE','PUBLISH','UPDATE_PUBLIC') AND expected_revision BETWEEN 1 AND 8999999999999998 AND applied_revision=expected_revision+1) OR(action='WITHDRAW' AND expected_revision>=1 AND applied_revision=least(expected_revision+1,8999999999999999)))))
;
CREATE FUNCTION birdtie_guard_human_moment_public_control() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable Moment operation';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':MOMENT:'||NEW.operation_id::text,144));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'current Person required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM moments m WHERE m.id=NEW.moment_id AND m.author_account_id=NEW.actor_account_id AND m.revision=NEW.applied_revision AND((NEW.action IN('CREATE','UPDATE') AND m.status='draft' AND m.visibility='private') OR(NEW.action='WITHDRAW' AND m.status='withdrawn' AND m.visibility='private') OR(NEW.action IN('PUBLISH','UPDATE_PUBLIC') AND m.status='published' AND m.visibility='public' AND m.place_id IS NOT NULL AND m.location_precision='place' AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at))) THEN RAISE EXCEPTION 'canonical owned Moment effect required';END IF;
 RETURN NEW;
END $$;
DROP TRIGGER human_moment_control_immutable ON human_moment_control_operations;
CREATE TRIGGER human_moment_control_immutable BEFORE INSERT OR UPDATE OR DELETE ON human_moment_control_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_human_moment_public_control();
COMMIT;
