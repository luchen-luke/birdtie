BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM moments WHERE status='published' AND visibility='public' AND place_id IS NULL AND location_precision='city') OR EXISTS(SELECT 1 FROM human_moment_control_operations op JOIN moments m ON m.id=op.moment_id WHERE op.action IN('PUBLISH','UPDATE_PUBLIC') AND m.place_id IS NULL AND m.location_precision='city') THEN RAISE EXCEPTION 'city Moment content or operation history cannot be discarded';END IF;
END $$;
CREATE OR REPLACE FUNCTION birdtie_guard_human_moment_public_control() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable Moment operation';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':MOMENT:'||NEW.operation_id::text,144));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'current Person required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM moments m WHERE m.id=NEW.moment_id AND m.author_account_id=NEW.actor_account_id AND m.revision=NEW.applied_revision AND((NEW.action IN('CREATE','UPDATE') AND m.status='draft' AND m.visibility='private') OR(NEW.action='WITHDRAW' AND m.status='withdrawn' AND m.visibility='private') OR(NEW.action IN('PUBLISH','UPDATE_PUBLIC') AND m.status='published' AND m.visibility='public' AND m.place_id IS NOT NULL AND m.location_precision='place' AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at))) THEN RAISE EXCEPTION 'canonical owned Moment effect required';END IF;
 RETURN NEW;
END $$;
DROP FUNCTION birdtie_city_public_moment_contract();
COMMIT;
