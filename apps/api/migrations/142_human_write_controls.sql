BEGIN;
ALTER TABLE organization_faqs ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 8999999999999999);
CREATE FUNCTION birdtie_faq_revision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.revision=OLD.revision THEN NEW.revision=least(OLD.revision+1,8999999999999999);
 ELSIF NEW.revision<>least(OLD.revision+1,8999999999999999) THEN RAISE EXCEPTION 'FAQ revision changed' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER faq_revision BEFORE UPDATE ON organization_faqs FOR EACH ROW EXECUTE FUNCTION birdtie_faq_revision();
CREATE TABLE person_declaration_control_epochs(owner_account_id uuid PRIMARY KEY REFERENCES accounts(id),version bigint NOT NULL CHECK(version BETWEEN 0 AND 8999999999999999),updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
INSERT INTO person_declaration_control_epochs(owner_account_id,version) SELECT pc.person_account_id,count(*) FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id WHERE c.context_type IN('CITY','INSTITUTION','ONLINE') GROUP BY pc.person_account_id;
CREATE FUNCTION birdtie_person_declaration_epoch() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE person uuid;context uuid; BEGIN
 IF TG_OP='DELETE' THEN person=OLD.person_account_id;context=OLD.context_id; ELSE person=NEW.person_account_id;context=NEW.context_id; END IF;
 IF EXISTS(SELECT 1 FROM contexts WHERE id=context AND context_type IN('CITY','INSTITUTION','ONLINE')) THEN
 INSERT INTO person_declaration_control_epochs(owner_account_id,version)VALUES(person,1) ON CONFLICT(owner_account_id) DO UPDATE SET version=least(person_declaration_control_epochs.version+1,8999999999999999),updated_at=clock_timestamp();
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER person_declaration_epoch AFTER INSERT OR UPDATE OR DELETE ON person_contexts FOR EACH ROW EXECUTE FUNCTION birdtie_person_declaration_epoch();
CREATE TABLE person_block_control_epochs(owner_account_id uuid NOT NULL REFERENCES accounts(id),target_account_id uuid NOT NULL REFERENCES accounts(id),version bigint NOT NULL CHECK(version BETWEEN 0 AND 8999999999999999),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(owner_account_id,target_account_id),CHECK(owner_account_id<>target_account_id));
INSERT INTO person_block_control_epochs(owner_account_id,target_account_id,version)
 SELECT actor_account_id,resource_id::uuid,least(count(*),8999999999999999) FROM audit_events WHERE resource_type='account' AND purpose='personal_safety' AND decision='allowed' AND action IN('block','unblock') AND resource_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' AND actor_account_id<>resource_id::uuid GROUP BY actor_account_id,resource_id;
INSERT INTO person_block_control_epochs(owner_account_id,target_account_id,version) SELECT blocker_account_id,blocked_account_id,1 FROM account_blocks WHERE blocker_account_id<>blocked_account_id ON CONFLICT DO NOTHING;
CREATE FUNCTION birdtie_person_block_epoch() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE person uuid;target uuid; BEGIN
 IF TG_OP='DELETE' THEN person=OLD.blocker_account_id;target=OLD.blocked_account_id; ELSE person=NEW.blocker_account_id;target=NEW.blocked_account_id; END IF;
 IF person<>target THEN INSERT INTO person_block_control_epochs(owner_account_id,target_account_id,version)VALUES(person,target,1) ON CONFLICT(owner_account_id,target_account_id) DO UPDATE SET version=least(person_block_control_epochs.version+1,8999999999999999),updated_at=clock_timestamp(); END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER person_block_epoch AFTER INSERT OR DELETE ON account_blocks FOR EACH ROW EXECUTE FUNCTION birdtie_person_block_epoch();
CREATE TABLE human_write_control_operations(
 actor_account_id uuid NOT NULL REFERENCES accounts(id),
 family text NOT NULL CHECK(family IN('FAQ','CONTEXT','REPORT','GRANT_REVOKE','BLOCK')),
 operation_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 scope_id text,
 action text,
 target_id text,
 result_id text,
 applied_version bigint NOT NULL DEFAULT 0 CHECK(applied_version BETWEEN 0 AND 8999999999999999),
 payload_sha256 bytea,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,family,operation_id),
 CHECK((status='STOPPED' AND scope_id IS NULL AND action IS NULL AND target_id IS NULL AND result_id IS NULL AND applied_version=0 AND payload_sha256 IS NULL)
 OR(status='SUCCEEDED' AND action IS NOT NULL AND payload_sha256 IS NOT NULL AND octet_length(payload_sha256)=32
 AND((family='FAQ' AND scope_id IS NOT NULL AND action IN('CREATE','UPDATE','DELETE') AND result_id IS NOT NULL AND applied_version>=1)
 OR(family='CONTEXT' AND action IN('DECLARE','REMOVE') AND result_id IS NOT NULL AND applied_version>=1)
 OR(family='REPORT' AND action='SUBMIT' AND result_id IS NOT NULL AND applied_version=0)
 OR(family='GRANT_REVOKE' AND action='REVOKE' AND target_id IS NOT NULL AND applied_version>=1)
 OR(family='BLOCK' AND action IN('BLOCK','UNBLOCK') AND target_id IS NOT NULL AND applied_version>=1))))
);
CREATE FUNCTION birdtie_human_write_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE valid boolean; BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'human operation receipt immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'human actor unavailable' USING ERRCODE='23514'; END IF;
 IF NEW.status='STOPPED' THEN RETURN NEW; END IF;
 IF NEW.family='FAQ' THEN
  IF NOT EXISTS(SELECT 1 FROM organization_memberships m JOIN organizations o ON o.id=m.organization_id WHERE o.id=NEW.scope_id::uuid AND m.user_account_id=NEW.actor_account_id AND m.status='active' AND m.role IN('owner','admin') AND o.status='active') THEN RAISE EXCEPTION 'FAQ current manager unavailable' USING ERRCODE='23514'; END IF;
  IF NEW.action='DELETE' THEN valid=NOT EXISTS(SELECT 1 FROM organization_faqs WHERE id=NEW.result_id::uuid);
  ELSE SELECT organization_id=NEW.scope_id::uuid AND revision=NEW.applied_version INTO valid FROM organization_faqs WHERE id=NEW.result_id::uuid; END IF;
 ELSIF NEW.family='CONTEXT' THEN SELECT version=NEW.applied_version INTO valid FROM person_declaration_control_epochs WHERE owner_account_id=NEW.actor_account_id;
 ELSIF NEW.family='REPORT' THEN SELECT reporter_account_id=NEW.actor_account_id INTO valid FROM incident_reports WHERE id=NEW.result_id::uuid;
 ELSIF NEW.family='GRANT_REVOKE' THEN SELECT owner_account_id=NEW.actor_account_id AND revoked_at IS NOT NULL AND revision=NEW.applied_version INTO valid FROM consent_grants WHERE id=NEW.target_id::uuid;
 ELSIF NEW.family='BLOCK' THEN SELECT version=NEW.applied_version INTO valid FROM person_block_control_epochs WHERE owner_account_id=NEW.actor_account_id AND target_account_id=NEW.target_id::uuid;valid=coalesce(valid,false) AND((NEW.action='BLOCK')=EXISTS(SELECT 1 FROM account_blocks WHERE blocker_account_id=NEW.actor_account_id AND blocked_account_id=NEW.target_id::uuid));
 END IF;
 IF valid IS DISTINCT FROM true THEN RAISE EXCEPTION 'human operation effect absent' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER human_write_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON human_write_control_operations FOR EACH ROW EXECUTE FUNCTION birdtie_human_write_operation_guard();
COMMIT;
