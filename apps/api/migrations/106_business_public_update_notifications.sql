BEGIN;
-- Real072 explicit public publication only; no Business Agent/runtime activation.
-- Existing072 audits remain unmarked. The marker is retained even when routing
-- decisions/Inbox are retired; used106 cannot silently discard retained events.
ALTER TABLE business_public_profile_audit ADD COLUMN notification_source boolean NOT NULL DEFAULT false;
ALTER TABLE business_public_profile_audit ADD CONSTRAINT business_public_notification_source_check CHECK(NOT notification_source OR action='publish');
ALTER TABLE native_notification_decisions ADD COLUMN business_public_update_id uuid REFERENCES business_public_profile_audit(id) ON DELETE CASCADE;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_business_public_source_check CHECK((kind='business_update')=(business_public_update_id IS NOT NULL));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check CHECK(num_nonnulls(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,community_message_id,activity_message_id,organization_membership_id,agent_task_id,business_claim_id,business_public_update_id)=1);
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check1;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check1 CHECK(source_id=COALESCE(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,community_message_id,activity_message_id,organization_membership_id,agent_task_id,business_claim_id,business_public_update_id));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_kind_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_kind_check CHECK(kind IN ('activity_reminder','activity_change','activity_cancelled','activity_review','place_review','direct_message','connection_request','connection_decision','community_message','activity_message','organization_invitation','organization_membership_change','agent_task_completed','agent_task_failed','opportunity_available','business_claim_review','business_update'));
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN ('place_candidate','activity_candidate','community','connection_request','conversation_message','activity_change','activity_cancelled','activity_reminder','community_message','activity_message','organization_membership','agent_task','opportunity_available','business_claim_review','business_update'));
CREATE FUNCTION birdtie_native_business_public_update(sid uuid,recipient uuid)
RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text)
LANGUAGE sql VOLATILE SET TIME ZONE 'UTC' AS $$
 WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT principal.id,b.id,'publication:'||audit.permission_version::text,
 encode(sha256(convert_to(jsonb_build_array(audit.id,audit.xmin::text,permission.xmin::text,
 source.source_snapshot,follow.id,follow.xmin::text,human.xmin::text,principal.xmin::text)::text,'UTF8')),'hex')
 FROM business_public_profile_audit audit CROSS JOIN stamp
 JOIN businesses b ON b.id=audit.business_id AND b.status='active' AND b.claim_status='verified'
 JOIN accounts principal ON principal.id=b.account_id AND principal.status='active' AND principal.account_type='business'
 JOIN accounts human ON human.id=recipient AND human.status='active' AND human.account_type='person'
 JOIN follows follow ON follow.business_id=b.id AND follow.follower_account_id=human.id AND follow.created_at<=audit.created_at
 JOIN LATERAL (SELECT p.version,p.facts,p.reviewed_at,p.valid_until,m.user_account_id,
 encode(sha256(convert_to(jsonb_build_array(to_jsonb(b),b.xmin::text,to_jsonb(principal),principal.xmin::text,
 to_jsonb(m),m.xmin::text,to_jsonb(owner_person),owner_person.xmin::text,
 to_jsonb(claim),claim.xmin::text,to_jsonb(p),p.xmin::text,
 to_jsonb(submitter),submitter.xmin::text,to_jsonb(sm),sm.xmin::text,
 to_jsonb(reviewer),reviewer.xmin::text,to_jsonb(g),g.xmin::text)::text,'UTF8')),'hex') AS source_snapshot
 FROM business_console_profiles p
 JOIN businesses b ON b.id=p.business_id AND b.status='active' AND b.claim_status='verified'
 JOIN accounts principal ON principal.id=b.account_id AND principal.account_type='business' AND principal.status='active'
 JOIN business_claim_controls claim ON claim.business_id=b.id AND claim.state='verified'
 JOIN business_memberships m ON m.business_id=b.id AND m.role='owner' AND m.status='active'
 JOIN accounts owner_person ON owner_person.id=m.user_account_id AND owner_person.account_type='person' AND owner_person.status='active'
 JOIN business_memberships sm ON sm.business_id=b.id AND sm.user_account_id=p.submitted_by AND sm.status='active' AND sm.role IN ('owner','admin')
 JOIN accounts submitter ON submitter.id=sm.user_account_id AND submitter.account_type='person' AND submitter.status='active'
 JOIN accounts reviewer ON reviewer.id=p.reviewed_by AND reviewer.account_type='person' AND reviewer.status='active'
 JOIN business_review_grants g ON g.business_id=b.id AND g.reviewer_account_id=reviewer.id AND g.state='active'
 AND 'profile'=ANY(g.permissions) AND g.valid_from<=stamp.n AND g.valid_until>stamp.n
 WHERE p.business_id=audit.business_id AND (NULL::uuid::uuid IS NULL OR m.user_account_id=NULL::uuid) AND p.state='verified'
 AND p.valid_until>stamp.n AND p.reviewed_at IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM business_memberships self WHERE self.business_id=b.id AND self.user_account_id=reviewer.id AND self.status='active')) source ON true
 JOIN business_public_profile_permissions permission ON permission.business_id=b.id AND permission.state='active'
 AND permission.version=audit.permission_version AND permission.profile_version=audit.profile_version
 AND permission.profile_version=source.version AND permission.source_snapshot=source.source_snapshot
 AND permission.approved_by=source.user_account_id AND permission.approved_by=audit.actor_account_id AND permission.valid_until>stamp.n
 WHERE audit.id=sid AND audit.action='publish' AND audit.notification_source AND audit.created_at<=stamp.n
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE
 (block.blocker_account_id=human.id AND block.blocked_account_id=principal.id) OR
 (block.blocker_account_id=principal.id AND block.blocked_account_id=human.id))
$$;
ALTER FUNCTION birdtie_native_notification_source(text,uuid,uuid) RENAME TO birdtie_native_notification_source_v105;
CREATE FUNCTION birdtie_native_notification_source(k text,sid uuid,recipient uuid)
RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text)
LANGUAGE plpgsql VOLATILE AS $$ BEGIN
 IF k='business_update' THEN RETURN QUERY SELECT * FROM birdtie_native_business_public_update(sid,recipient);RETURN;END IF;
 RETURN QUERY SELECT * FROM birdtie_native_notification_source_v105(k,sid,recipient);
END $$;
CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record; current_policy record; expected_cat text;
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
    expected_cat:=CASE WHEN NEW.kind IN ('business_claim_review','business_update') THEN 'BUSINESS' WHEN NEW.kind='opportunity_available' THEN 'ACTIVITY' WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY'
        WHEN NEW.kind IN ('direct_message','activity_message') THEN 'MESSAGE'
        WHEN NEW.kind IN ('connection_request','connection_decision') THEN 'SOCIAL'
        WHEN NEW.kind='community_message' THEN 'COMMUNITY'
        WHEN NEW.kind LIKE 'organization_%' THEN 'ORGANIZATION'
        WHEN NEW.kind LIKE 'agent_task_%' THEN 'AGENT' WHEN NEW.kind='place_review' THEN 'SYSTEM' END;
    SELECT * INTO current_source FROM birdtie_native_notification_source(NEW.kind,NEW.source_id,NEW.recipient_id);
    IF NOT FOUND OR NEW.category IS DISTINCT FROM expected_cat OR NEW.actor_id IS DISTINCT FROM current_source.actor_id
       OR NEW.event_version IS DISTINCT FROM current_source.event_version OR NEW.source_version IS DISTINCT FROM current_source.source_version THEN
        RAISE EXCEPTION 'notification current source unavailable' USING ERRCODE='23514';
    END IF;
    SELECT * INTO current_policy FROM birdtie_native_notification_preference(NEW.recipient_id,NEW.category);
    IF NOT FOUND OR NEW.policy_version<>current_policy.version OR NEW.disposition<>current_policy.disposition
       OR NEW.priority<>current_policy.priority OR NEW.reason<>current_policy.reason THEN
        RAISE EXCEPTION 'notification policy changed' USING ERRCODE='23514';
    END IF;
    NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
-- Business Follow is not removed by the old Person block trigger. Permanently
-- retire matching public notices on either-direction block; unblock cannot
-- resurrect them.099 delivered receipt rows/rolling quota are NOT deleted.
CREATE FUNCTION birdtie_retire_business_public_notifications() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 DELETE FROM native_notification_decisions d USING business_public_profile_audit audit,businesses b
 WHERE d.kind='business_update' AND d.business_public_update_id=audit.id AND audit.business_id=b.id
 AND ((d.recipient_id=NEW.blocker_account_id AND b.account_id=NEW.blocked_account_id)
 OR (d.recipient_id=NEW.blocked_account_id AND b.account_id=NEW.blocker_account_id));
 RETURN NEW;
END $$;
CREATE TRIGGER account_block_retires_business_public_notifications AFTER INSERT OR UPDATE OF blocker_account_id,blocked_account_id ON account_blocks
FOR EACH ROW EXECUTE FUNCTION birdtie_retire_business_public_notifications();
COMMIT;
