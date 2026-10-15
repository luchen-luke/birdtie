BEGIN;
CREATE TABLE moment_likes(
 moment_id uuid NOT NULL REFERENCES moments(id),actor_account_id uuid NOT NULL REFERENCES accounts(id),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 8999999999999999),liked boolean NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(moment_id,actor_account_id)
);
CREATE TABLE moment_comments(
 id uuid PRIMARY KEY,moment_id uuid NOT NULL REFERENCES moments(id),author_account_id uuid NOT NULL REFERENCES accounts(id),
 body text NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 2000),version bigint NOT NULL CHECK(version BETWEEN 1 AND 8999999999999999),
 deleted boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX moment_comments_history ON moment_comments(moment_id,created_at DESC,id DESC) WHERE NOT deleted;
CREATE TABLE moment_interaction_operations(
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,event_id uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),action text,moment_id uuid REFERENCES moments(id),comment_id uuid REFERENCES moment_comments(id),
 applied_version bigint NOT NULL DEFAULT 0,payload_sha256 bytea,notification_source boolean NOT NULL DEFAULT false,committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='STOPPED' AND action IS NULL AND moment_id IS NULL AND comment_id IS NULL AND applied_version=0 AND payload_sha256 IS NULL AND NOT notification_source) OR(status='SUCCEEDED' AND action IN('LIKE','UNLIKE','COMMENT','DELETE_COMMENT') AND moment_id IS NOT NULL AND applied_version BETWEEN 1 AND 8999999999999999 AND octet_length(payload_sha256)=32 AND ((action IN('LIKE','UNLIKE') AND comment_id IS NULL) OR(action IN('COMMENT','DELETE_COMMENT') AND comment_id IS NOT NULL)) AND(NOT notification_source OR action IN('LIKE','COMMENT'))))
);
CREATE TABLE inbox_management_operations(
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 action text,affected_count integer NOT NULL DEFAULT 0 CHECK(affected_count>=0),payload_sha256 bytea,cutoff timestamptz,source_version text,committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='STOPPED' AND action IS NULL AND affected_count=0 AND payload_sha256 IS NULL AND cutoff IS NULL AND source_version IS NULL) OR(status='SUCCEEDED' AND action IN('MARK_ALL_READ','DELETE_ITEM','CLEAR_READ') AND octet_length(payload_sha256)=32 AND cutoff IS NOT NULL AND source_version ~ '^[0-9a-f]{64}$'))
);
CREATE FUNCTION birdtie_guard_moment_interaction_operation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original interaction operation is immutable';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':MOMENT_INTERACTION:'||NEW.operation_id::text,151));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'active Person required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT ((NEW.action IN('LIKE','UNLIKE') AND EXISTS(SELECT 1 FROM moment_likes l WHERE l.moment_id=NEW.moment_id AND l.actor_account_id=NEW.actor_account_id AND l.version=NEW.applied_version AND l.liked=(NEW.action='LIKE'))) OR(NEW.action IN('COMMENT','DELETE_COMMENT') AND EXISTS(SELECT 1 FROM moment_comments c WHERE c.id=NEW.comment_id AND c.moment_id=NEW.moment_id AND c.author_account_id=NEW.actor_account_id AND c.version=NEW.applied_version AND c.deleted=(NEW.action='DELETE_COMMENT')))) THEN RAISE EXCEPTION 'matching canonical interaction effect required';END IF;
 RETURN NEW;END $$;
CREATE TRIGGER moment_interaction_operations_immutable BEFORE INSERT OR UPDATE OR DELETE ON moment_interaction_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_interaction_operation();
CREATE FUNCTION birdtie_guard_inbox_management_operation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original Inbox management operation is immutable';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':INBOX_MANAGEMENT:'||NEW.operation_id::text,151));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'active Person required';END IF;RETURN NEW;END $$;
CREATE TRIGGER inbox_management_operations_immutable BEFORE INSERT OR UPDATE OR DELETE ON inbox_management_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_inbox_management_operation();

-- Text readership is independent of the presence of a photograph. Place
-- sources reuse the exact retained-evidence predicate installed by 145.
CREATE FUNCTION birdtie_moment_public_binding(mid uuid,viewer uuid,read_at timestamptz) RETURNS text LANGUAGE sql VOLATILE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(m.xmin::text,a.xmin::text,c.xmin::text,source.binding,
 (SELECT account.xmin::text FROM accounts account WHERE account.id=viewer),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb) FROM person_block_control_epochs ep WHERE viewer IS NOT NULL AND((ep.owner_account_id=viewer AND ep.target_account_id=a.id) OR(ep.owner_account_id=a.id AND ep.target_account_id=viewer))))::text,'UTF8')),'hex')
 FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN cities c ON c.id=m.city_id
 LEFT JOIN LATERAL birdtie_public_moment_photo_source(m.id,read_at) source ON true
 WHERE m.id=mid AND m.status='published' AND m.visibility='public' AND m.revision>=2
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=read_at
 AND a.account_type='person' AND a.status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>read_at)
 AND((m.place_id IS NULL AND m.location_precision='city') OR(m.place_id IS NOT NULL AND m.location_precision='place' AND source.binding IS NOT NULL))
 AND(viewer IS NULL OR EXISTS(SELECT 1 FROM accounts account WHERE account.id=viewer AND account.account_type='person' AND account.status='active'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND((b.blocker_account_id=viewer AND b.blocked_account_id=a.id) OR(b.blocker_account_id=a.id AND b.blocked_account_id=viewer)))
$$;
ALTER TABLE native_notification_decisions ADD COLUMN moment_interaction_event_id uuid REFERENCES moment_interaction_operations(event_id);
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check CHECK(num_nonnulls(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,community_message_id,activity_message_id,organization_membership_id,agent_task_id,business_claim_id,business_public_update_id,moment_interaction_event_id)=1);
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check1;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check1 CHECK(source_id=COALESCE(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,community_message_id,activity_message_id,organization_membership_id,agent_task_id,business_claim_id,business_public_update_id,moment_interaction_event_id));
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_moment_source_check CHECK((kind IN('moment_like','moment_comment'))=(moment_interaction_event_id IS NOT NULL));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_kind_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_kind_check CHECK(kind IN('activity_reminder','activity_change','activity_cancelled','activity_review','place_review','direct_message','connection_request','connection_decision','community_message','activity_message','organization_invitation','organization_membership_change','agent_task_completed','agent_task_failed','opportunity_available','business_claim_review','business_update','moment_like','moment_comment'));
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN('place_candidate','activity_candidate','community','connection_request','conversation_message','activity_change','activity_cancelled','activity_reminder','community_message','activity_message','organization_membership','agent_task','opportunity_available','business_claim_review','business_update','moment_like','moment_comment'));
CREATE FUNCTION birdtie_native_moment_interaction_source(sid uuid,recipient uuid) RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text) LANGUAGE sql VOLATILE AS $$
 WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT op.actor_account_id,m.id,op.event_id::text,
 encode(sha256(convert_to(jsonb_build_array(op.event_id,op.xmin::text,birdtie_moment_public_binding(m.id,recipient,stamp.at),actor.xmin::text,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb) FROM person_block_control_epochs ep WHERE (ep.owner_account_id=recipient AND ep.target_account_id=op.actor_account_id) OR(ep.owner_account_id=op.actor_account_id AND ep.target_account_id=recipient)))::text,'UTF8')),'hex')
 FROM moment_interaction_operations op JOIN moments m ON m.id=op.moment_id JOIN accounts actor ON actor.id=op.actor_account_id CROSS JOIN stamp
 WHERE op.event_id=sid AND op.status='SUCCEEDED' AND op.notification_source AND m.author_account_id=recipient AND op.actor_account_id<>recipient
 AND actor.account_type='person' AND actor.status='active' AND birdtie_moment_public_binding(m.id,recipient,stamp.at) IS NOT NULL AND birdtie_moment_public_binding(m.id,op.actor_account_id,stamp.at) IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=recipient AND b.blocked_account_id=op.actor_account_id) OR(b.blocker_account_id=op.actor_account_id AND b.blocked_account_id=recipient))
 AND((op.action='LIKE' AND EXISTS(SELECT 1 FROM moment_likes l WHERE l.moment_id=m.id AND l.actor_account_id=op.actor_account_id AND l.liked AND l.version=op.applied_version)) OR(op.action='COMMENT' AND EXISTS(SELECT 1 FROM moment_comments comment WHERE comment.id=op.comment_id AND NOT comment.deleted AND comment.version=op.applied_version)))
$$;
ALTER FUNCTION birdtie_native_notification_source(text,uuid,uuid) RENAME TO birdtie_native_notification_source_v150;
CREATE FUNCTION birdtie_native_notification_source(k text,sid uuid,recipient uuid) RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text) LANGUAGE plpgsql VOLATILE AS $$ BEGIN
 IF k IN('moment_like','moment_comment') THEN RETURN QUERY SELECT source.* FROM birdtie_native_moment_interaction_source(sid,recipient) source WHERE EXISTS(SELECT 1 FROM moment_interaction_operations op WHERE op.event_id=sid AND op.action=CASE k WHEN 'moment_like' THEN 'LIKE' ELSE 'COMMENT' END);RETURN;END IF;
 RETURN QUERY SELECT * FROM birdtie_native_notification_source_v150(k,sid,recipient);END $$;
CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record;current_policy record;expected_cat text;BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
 expected_cat:=CASE WHEN NEW.kind IN('business_claim_review','business_update') THEN 'BUSINESS' WHEN NEW.kind='opportunity_available' THEN 'ACTIVITY' WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY' WHEN NEW.kind IN('direct_message','activity_message') THEN 'MESSAGE' WHEN NEW.kind IN('connection_request','connection_decision','moment_like','moment_comment') THEN 'SOCIAL' WHEN NEW.kind='community_message' THEN 'COMMUNITY' WHEN NEW.kind LIKE 'organization_%' THEN 'ORGANIZATION' WHEN NEW.kind LIKE 'agent_task_%' THEN 'AGENT' WHEN NEW.kind='place_review' THEN 'SYSTEM' END;
 SELECT * INTO current_source FROM birdtie_native_notification_source(NEW.kind,NEW.source_id,NEW.recipient_id);
 IF NOT FOUND OR NEW.category IS DISTINCT FROM expected_cat OR NEW.actor_id IS DISTINCT FROM current_source.actor_id OR NEW.event_version IS DISTINCT FROM current_source.event_version OR NEW.source_version IS DISTINCT FROM current_source.source_version THEN RAISE EXCEPTION 'notification current source unavailable' USING ERRCODE='23514';END IF;
 SELECT * INTO current_policy FROM birdtie_native_notification_preference(NEW.recipient_id,NEW.category);
 IF NOT FOUND OR NEW.policy_version<>current_policy.version OR NEW.disposition<>current_policy.disposition OR NEW.priority<>current_policy.priority OR NEW.reason<>current_policy.reason THEN RAISE EXCEPTION 'notification policy changed' USING ERRCODE='23514';END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;END $$;
CREATE FUNCTION birdtie_moment_interaction_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'moment-interaction-v1'::text $$;
COMMIT;
