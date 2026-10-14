BEGIN;
-- Only this zero-effect own namespace is independent of current entity role.
-- Effectful receipts still require the current manager and exact saved version.
CREATE OR REPLACE FUNCTION birdtie_entity_cover_operation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'cover operation history is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.status='STOPPED' THEN
  IF NEW.action<>'STOP' OR NEW.applied_version<>0 OR NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=NEW.actor_account_id AND a.account_type='person' AND a.status='active') THEN RAISE EXCEPTION 'cover stop requires zero effect and current Person' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.status<>'SUCCEEDED' OR NOT birdtie_entity_cover_manage(NEW.entity_type,NEW.entity_id,NEW.actor_account_id) THEN RAISE EXCEPTION 'cover operation needs current manager' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM entity_covers c WHERE c.entity_type=NEW.entity_type AND c.entity_id=NEW.entity_id AND c.version=NEW.applied_version AND c.uploaded_by=NEW.actor_account_id AND((NEW.action='SAVE' AND c.thumbnail IS NOT NULL) OR(NEW.action='DELETE' AND c.thumbnail IS NULL))) THEN RAISE EXCEPTION 'cover receipt must identify actual effect' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE UNIQUE INDEX entity_cover_one_original_effect ON entity_cover_operations(entity_type,entity_id,applied_version) WHERE status='SUCCEEDED';
COMMIT;
