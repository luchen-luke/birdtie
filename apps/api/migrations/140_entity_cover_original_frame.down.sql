BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM entity_cover_operations WHERE status='STOPPED') THEN RAISE EXCEPTION 'original stopped cover history prevents downgrade'; END IF; END $$;
DROP INDEX entity_cover_one_original_effect;
CREATE OR REPLACE FUNCTION birdtie_entity_cover_operation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'cover operation history is immutable' USING ERRCODE='23514'; END IF;
 IF NOT birdtie_entity_cover_manage(NEW.entity_type,NEW.entity_id,NEW.actor_account_id) THEN RAISE EXCEPTION 'cover operation needs current manager' USING ERRCODE='23514'; END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM entity_covers c WHERE c.entity_type=NEW.entity_type AND c.entity_id=NEW.entity_id AND c.version=NEW.applied_version) THEN RAISE EXCEPTION 'cover receipt must identify actual effect' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
COMMIT;
