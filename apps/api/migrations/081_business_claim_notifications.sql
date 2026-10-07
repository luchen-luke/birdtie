BEGIN;
-- Original human business claim review only; no Business Agent activation.
ALTER TABLE native_notification_decisions ADD COLUMN business_claim_id uuid REFERENCES business_claim_controls(business_id) ON DELETE CASCADE;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK ((resource_type = ANY (ARRAY['place_candidate'::text, 'activity_candidate'::text, 'community'::text, 'connection_request'::text, 'conversation_message'::text, 'activity_change'::text, 'activity_cancelled'::text, 'activity_reminder'::text, 'community_message'::text, 'activity_message'::text, 'organization_membership'::text, 'agent_task'::text, 'opportunity_available'::text, 'business_claim_review'::text])));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check CHECK ((num_nonnulls(activity_id, activity_candidate_id, place_candidate_id, message_id, connection_request_id, community_message_id, activity_message_id, organization_membership_id, agent_task_id, business_claim_id) = 1));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_check1;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_check1 CHECK ((source_id = COALESCE(activity_id, activity_candidate_id, place_candidate_id, message_id, connection_request_id, community_message_id, activity_message_id, organization_membership_id, agent_task_id, business_claim_id)));
ALTER TABLE native_notification_decisions DROP CONSTRAINT native_notification_decisions_kind_check;
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_decisions_kind_check CHECK ((kind = ANY (ARRAY['activity_reminder'::text, 'activity_change'::text, 'activity_cancelled'::text, 'activity_review'::text, 'place_review'::text, 'direct_message'::text, 'connection_request'::text, 'connection_decision'::text, 'community_message'::text, 'activity_message'::text, 'organization_invitation'::text, 'organization_membership_change'::text, 'agent_task_completed'::text, 'agent_task_failed'::text, 'opportunity_available'::text, 'business_claim_review'::text])));
ALTER TABLE native_notification_decisions ADD CONSTRAINT native_notification_business_claim_source_check CHECK((kind='business_claim_review')=(business_claim_id IS NOT NULL));
ALTER FUNCTION birdtie_native_notification_source(text,uuid,uuid) RENAME TO birdtie_native_notification_legacy_source_v079;
CREATE FUNCTION birdtie_native_notification_source(k text,sid uuid,recipient uuid)
RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text)
LANGUAGE plpgsql VOLATILE AS $$
BEGIN
 IF k<>'business_claim_review' THEN
  RETURN QUERY SELECT * FROM birdtie_native_notification_legacy_source_v079(k,sid,recipient); RETURN;
 END IF;
 RETURN QUERY WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS n)
 SELECT reviewer.id, b.id, 'claim:'||claim.version::text,
 encode(sha256(convert_to(jsonb_build_array(claim.xmin::text,b.xmin::text,
 principal.xmin::text,human.xmin::text,member.xmin::text,reviewer.xmin::text,
 rg.xmin::text,audit.xmin::text)::text,'UTF8')),'hex')
 FROM business_claim_controls claim CROSS JOIN stamp
 JOIN businesses b ON b.id=claim.business_id AND b.status='active' AND b.claim_status=claim.state
 JOIN accounts principal ON principal.id=b.account_id AND principal.account_type='business' AND principal.status='active'
 JOIN accounts human ON human.id=recipient AND human.account_type='person' AND human.status='active'
 JOIN business_memberships member ON member.business_id=b.id AND member.user_account_id=human.id
  AND member.status='active' AND member.role IN ('owner','admin')
 JOIN accounts reviewer ON reviewer.id=claim.reviewed_by AND reviewer.account_type='person' AND reviewer.status='active'
 JOIN business_review_grants rg ON rg.business_id=b.id AND rg.reviewer_account_id=reviewer.id
  AND rg.state='active' AND 'claim'=ANY(rg.permissions) AND rg.valid_from<=stamp.n AND rg.valid_until>stamp.n
 JOIN business_console_audit_events audit ON audit.business_id=b.id AND audit.resource_id=b.id
  AND audit.resource_version=claim.version AND audit.actor_account_id=reviewer.id
  AND audit.action=CASE claim.state WHEN 'verified' THEN 'claim_approve' WHEN 'rejected' THEN 'claim_reject' WHEN 'revoked' THEN 'claim_revoke' END
 WHERE claim.business_id=sid AND claim.submitted_by=human.id AND claim.reviewed_by<>human.id
 AND claim.state IN ('verified','rejected','revoked') AND claim.reviewed_at<=stamp.n
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE
  (bl.blocker_account_id=human.id AND bl.blocked_account_id=reviewer.id) OR
  (bl.blocker_account_id=reviewer.id AND bl.blocked_account_id=human.id));
END $$;
CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record; current_policy record; expected_cat text;
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
    expected_cat:=CASE WHEN NEW.kind='business_claim_review' THEN 'BUSINESS' WHEN NEW.kind='opportunity_available' THEN 'ACTIVITY' WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY'
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
COMMIT;
