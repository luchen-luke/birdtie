BEGIN;
CREATE FUNCTION birdtie_community_interest_audit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='TRUNCATE' THEN
  IF EXISTS(SELECT 1 FROM audit_events WHERE resource_type='person_community_declaration' OR purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION') THEN
   RAISE EXCEPTION 'community interest audit history prevents truncate';
  END IF;
  RETURN NULL;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') AND (OLD.resource_type='person_community_declaration' OR OLD.purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION') THEN
  RAISE EXCEPTION 'community interest audit is append only';
 END IF;
 IF TG_OP <> 'DELETE' AND (NEW.resource_type='person_community_declaration' OR NEW.purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION') THEN
  IF TG_OP='UPDATE' OR NEW.resource_type<>'person_community_declaration' OR NEW.purpose<>'HUMAN_COMMUNITY_INTEREST_DECLARATION' OR NEW.decision<>'allowed' OR NEW.action NOT IN ('PUBLIC','PRIVATE','DELETE') OR NEW.resource_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:interest$' OR NEW.actor_account_id IS NULL OR NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN
   RAISE EXCEPTION 'invalid community interest audit';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
CREATE TRIGGER community_interest_audit_guard BEFORE INSERT OR UPDATE OR DELETE ON audit_events FOR EACH ROW EXECUTE FUNCTION birdtie_community_interest_audit_guard();
CREATE TRIGGER community_interest_audit_truncate_guard BEFORE TRUNCATE ON audit_events FOR EACH STATEMENT EXECUTE FUNCTION birdtie_community_interest_audit_guard();
CREATE INDEX community_interest_audit_epoch ON audit_events(actor_account_id,resource_id,id DESC) WHERE resource_type='person_community_declaration' AND purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION';
COMMIT;
