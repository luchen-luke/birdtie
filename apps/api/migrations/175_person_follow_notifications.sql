BEGIN;

-- Original native follow events only. No backfill and no trigger on follows:
-- legacy imports and pre-upgrade follows must never be replayed as new notices.
CREATE TABLE person_follow_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 source_follow_id uuid NOT NULL UNIQUE,
 actor_account_id uuid NOT NULL REFERENCES accounts(id),
 recipient_account_id uuid NOT NULL REFERENCES accounts(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(actor_account_id<>recipient_account_id)
);
CREATE FUNCTION birdtie_person_follow_event_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'follow event immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM follows f WHERE f.id=NEW.source_follow_id
   AND f.follower_account_id=NEW.actor_account_id AND f.person_account_id=NEW.recipient_account_id
   AND f.legacy_source_id IS NULL) THEN
   RAISE EXCEPTION 'native follow source unavailable' USING ERRCODE='23514';
 END IF;
 NEW.created_at:=clock_timestamp(); RETURN NEW;
END $$;
CREATE TRIGGER person_follow_event_guard BEFORE INSERT OR UPDATE OR DELETE ON person_follow_events FOR EACH ROW EXECUTE FUNCTION birdtie_person_follow_event_guard();

ALTER TABLE native_notification_decisions ADD COLUMN person_follow_event_id uuid REFERENCES person_follow_events(id);
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check CHECK(num_nonnulls(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,community_message_id,activity_message_id,organization_membership_id,agent_task_id,business_claim_id,business_public_update_id,moment_interaction_event_id,person_follow_event_id)=1);
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check1;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check1 CHECK(source_id=COALESCE(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,community_message_id,activity_message_id,organization_membership_id,agent_task_id,business_claim_id,business_public_update_id,moment_interaction_event_id,person_follow_event_id));
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_person_follow_source_check CHECK((kind='person_follow')=(person_follow_event_id IS NOT NULL));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_kind_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_kind_check CHECK(kind IN('activity_reminder','activity_change','activity_cancelled','activity_review','place_review','direct_message','connection_request','connection_decision','community_message','activity_message','organization_invitation','organization_membership_change','agent_task_completed','agent_task_failed','opportunity_available','business_claim_review','business_update','moment_like','moment_comment','person_follow'));
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN('place_candidate','activity_candidate','community','connection_request','conversation_message','activity_change','activity_cancelled','activity_reminder','community_message','activity_message','organization_membership','agent_task','opportunity_available','business_claim_review','business_update','moment_like','moment_comment','legacy_history','person_follow'));

-- Generic notification text reveals no private Profile fields. The destination
-- resolver independently checks current Profile visibility before releasing ID.
CREATE FUNCTION birdtie_native_person_follow_source(sid uuid,recipient uuid)
RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text) LANGUAGE sql VOLATILE AS $$
 SELECT ev.actor_account_id,NULL::uuid,ev.id::text,
 encode(sha256(convert_to(jsonb_build_array(ev.id,ev.xmin::text,f.id,f.xmin::text,a.xmin::text,r.xmin::text,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb)
  FROM person_block_control_epochs ep WHERE (ep.owner_account_id=recipient AND ep.target_account_id=ev.actor_account_id) OR(ep.owner_account_id=ev.actor_account_id AND ep.target_account_id=recipient)))::text,'UTF8')),'hex')
 FROM person_follow_events ev JOIN follows f ON f.id=ev.source_follow_id
 AND f.follower_account_id=ev.actor_account_id AND f.person_account_id=ev.recipient_account_id AND f.legacy_source_id IS NULL
 JOIN accounts a ON a.id=ev.actor_account_id AND a.account_type='person' AND a.status='active'
 JOIN accounts r ON r.id=ev.recipient_account_id AND r.account_type='person' AND r.status='active'
 WHERE ev.id=sid AND ev.recipient_account_id=recipient
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=recipient AND b.blocked_account_id=ev.actor_account_id) OR(b.blocker_account_id=ev.actor_account_id AND b.blocked_account_id=recipient))
$$;
ALTER FUNCTION birdtie_native_notification_source(text,uuid,uuid) RENAME TO birdtie_native_notification_source_v174;
CREATE FUNCTION birdtie_native_notification_source(k text,sid uuid,recipient uuid) RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text) LANGUAGE plpgsql VOLATILE AS $$ BEGIN
 IF k='person_follow' THEN RETURN QUERY SELECT * FROM birdtie_native_person_follow_source(sid,recipient); RETURN; END IF;
 RETURN QUERY SELECT * FROM birdtie_native_notification_source_v174(k,sid,recipient);
END $$;

CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record;current_policy record;expected_cat text;BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
 expected_cat:=CASE WHEN NEW.kind IN('business_claim_review','business_update') THEN 'BUSINESS' WHEN NEW.kind='opportunity_available' THEN 'ACTIVITY' WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY' WHEN NEW.kind IN('direct_message','activity_message') THEN 'MESSAGE' WHEN NEW.kind IN('connection_request','connection_decision','moment_like','moment_comment','person_follow') THEN 'SOCIAL' WHEN NEW.kind='community_message' THEN 'COMMUNITY' WHEN NEW.kind LIKE 'organization_%' THEN 'ORGANIZATION' WHEN NEW.kind LIKE 'agent_task_%' THEN 'AGENT' WHEN NEW.kind='place_review' THEN 'SYSTEM' END;
 SELECT * INTO current_source FROM birdtie_native_notification_source(NEW.kind,NEW.source_id,NEW.recipient_id);
 IF NOT FOUND OR NEW.category IS DISTINCT FROM expected_cat OR NEW.actor_id IS DISTINCT FROM current_source.actor_id OR NEW.event_version IS DISTINCT FROM current_source.event_version OR NEW.source_version IS DISTINCT FROM current_source.source_version THEN RAISE EXCEPTION 'notification current source unavailable' USING ERRCODE='23514';END IF;
 SELECT * INTO current_policy FROM birdtie_native_notification_preference(NEW.recipient_id,NEW.category);
 IF NOT FOUND OR NEW.policy_version<>current_policy.version OR NEW.disposition<>current_policy.disposition OR NEW.priority<>current_policy.priority OR NEW.reason<>current_policy.reason THEN RAISE EXCEPTION 'notification policy changed' USING ERRCODE='23514';END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;END $$;
COMMIT;
