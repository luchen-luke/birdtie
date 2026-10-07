BEGIN;
DO $$ DECLARE registry_name text; activity_name text; BEGIN
 SELECT conname INTO STRICT registry_name FROM pg_constraint WHERE conrelid='native_notification_decisions'::regclass AND contype='c' AND position('activity_reminder' in pg_get_constraintdef(oid))>0 AND position('activity_id' in pg_get_constraintdef(oid))=0;
 SELECT conname INTO STRICT activity_name FROM pg_constraint WHERE conrelid='native_notification_decisions'::regclass AND contype='c' AND position('activity_reminder' in pg_get_constraintdef(oid))>0 AND position('activity_id' in pg_get_constraintdef(oid))>0;
 EXECUTE format('ALTER TABLE native_notification_decisions DROP CONSTRAINT %I',registry_name);
 EXECUTE format('ALTER TABLE native_notification_decisions ADD CONSTRAINT %I CHECK(kind IN (''activity_reminder'',''activity_change'',''activity_cancelled'',''activity_review'',''place_review'',''direct_message'',''connection_request'',''connection_decision'',''community_message'',''activity_message'',''organization_invitation'',''organization_membership_change'',''agent_task_completed'',''agent_task_failed'',''opportunity_available''))',registry_name);
 EXECUTE format('ALTER TABLE native_notification_decisions DROP CONSTRAINT %I',activity_name);
 EXECUTE format('ALTER TABLE native_notification_decisions ADD CONSTRAINT %I CHECK((kind IN (''activity_reminder'',''activity_change'',''activity_cancelled'',''opportunity_available''))=(activity_id IS NOT NULL))',activity_name);
END $$;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN ('place_candidate','activity_candidate','community','connection_request','conversation_message','activity_change','activity_cancelled','activity_reminder','community_message','activity_message','organization_membership','agent_task','opportunity_available'));
-- This ordinary notification source reads exact current native facts. It
-- is not a model score, new Opportunity entity, Agent purpose or grant.
ALTER FUNCTION birdtie_native_notification_source(text,uuid,uuid) RENAME TO birdtie_native_notification_legacy_source_v061;
CREATE FUNCTION birdtie_native_notification_source(k text,sid uuid,recipient uuid)
RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text)
LANGUAGE plpgsql VOLATILE AS $$
BEGIN
 IF k<>'opportunity_available' THEN
  RETURN QUERY SELECT * FROM birdtie_native_notification_legacy_source_v061(k,sid,recipient);RETURN;
 END IF;
 RETURN QUERY WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS n),
 owned AS MATERIALIZED(SELECT si.*,si.xmin::text AS source_token FROM social_intents si WHERE si.creator_account_id=recipient ORDER BY si.created_at DESC,si.id DESC LIMIT 100),
 qualified AS MATERIALIZED(
 SELECT si.id AS intent_id,si.source_token AS intent_token,a.host_account_id AS actor,a.id AS context,
 a.revision AS activity_revision,
 jsonb_build_array(si.source_token,a.xmin::text,recipient_account.xmin::text,host.xmin::text,
 p.xmin::text,city.xmin::text,target.xmin::text,chosen_ctx.xmin::text,ao.xmin::text,
 co.xmin::text,community_owner.xmin::text,org.xmin::text,org_principal.xmin::text,biz.xmin::text,biz_principal.xmin::text,
 (SELECT jsonb_agg(jsonb_build_array(relation.xmin::text,venue.xmin::text,candidate.xmin::text) ORDER BY relation.place_id) FROM business_venue_relations relation JOIN venues venue ON venue.place_id=relation.place_id JOIN venue_candidates candidate ON candidate.id=venue.source_candidate_id WHERE relation.business_id=biz.id AND relation.place_id=p.id AND relation.status='verified'),
 (SELECT jsonb_agg(jsonb_build_array(t.xmin::text,r.xmin::text) ORDER BY t.id) FROM person_ties t JOIN connection_requests r ON r.id=t.request_id WHERE t.status='active' AND r.scope='friend' AND r.state='accepted' AND t.person_a_account_id=LEAST(recipient,a.host_account_id) AND t.person_b_account_id=GREATEST(recipient,a.host_account_id) AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id),
 (SELECT jsonb_agg(ai.xmin::text ORDER BY ai.id) FROM activity_invitations ai WHERE ai.activity_id=a.id AND ai.invitee_account_id=recipient AND ai.status='invited'),
 (SELECT jsonb_agg(cm.xmin::text ORDER BY cm.id) FROM community_memberships cm WHERE cm.community_id=ao.community_id AND cm.user_account_id=recipient AND cm.status='active'),
 (SELECT jsonb_agg(om.xmin::text ORDER BY om.id) FROM organization_memberships om WHERE om.organization_id=ao.organization_id AND om.user_account_id=recipient AND om.status='active'),
 (SELECT jsonb_agg(bm.xmin::text ORDER BY bm.id) FROM business_memberships bm WHERE bm.business_id=ao.business_id AND bm.user_account_id=recipient AND bm.status='active')) AS fingerprint,
 si.created_at AS intent_created
 FROM owned si CROSS JOIN stamp
 JOIN accounts recipient_account ON recipient_account.id=recipient AND recipient_account.account_type='person' AND recipient_account.status='active'
 JOIN activities a ON a.id=sid AND a.host_account_id<>recipient
 JOIN accounts host ON host.id=a.host_account_id AND host.status='active'
 JOIN activity_organizers ao ON ao.activity_id=a.id
 JOIN places p ON p.id=a.place_id AND p.city_id=a.city_id AND p.publication_status='published' AND (p.expires_at IS NULL OR (isfinite(p.expires_at) AND p.expires_at>stamp.n))
 JOIN cities city ON city.id=a.city_id AND city.publication_status='published' AND (city.expires_at IS NULL OR (isfinite(city.expires_at) AND city.expires_at>stamp.n))
 LEFT JOIN social_intent_audience_targets target ON target.intent_id=si.id
 LEFT JOIN contexts chosen_ctx ON chosen_ctx.id=si.context_id AND chosen_ctx.context_type='CITY'
 LEFT JOIN communities co ON co.id=ao.community_id
 LEFT JOIN organizations org ON org.id=ao.organization_id
 LEFT JOIN businesses biz ON biz.id=ao.business_id
 LEFT JOIN accounts community_owner ON community_owner.id=co.owner_account_id
 LEFT JOIN accounts org_principal ON org_principal.id=org.account_id
 LEFT JOIN accounts biz_principal ON biz_principal.id=biz.account_id
 WHERE si.intent_type='FIND_ACTIVITY' AND si.status='ACTIVE' AND isfinite(si.expires_at) AND si.expires_at>stamp.n AND si.modality='IN_PERSON'
 -- Explicit City scope is required; no map center, residence or inferred City.
 AND coalesce(target.city_id,chosen_ctx.city_id) IS NOT NULL
 AND coalesce(target.city_id,chosen_ctx.city_id)=a.city_id
 AND (si.context_id IS NULL OR chosen_ctx.id IS NOT NULL)
 AND (target.city_id IS NULL OR chosen_ctx.city_id IS NULL OR target.city_id=chosen_ctx.city_id)
 AND (si.audience<>'LOCAL' OR target.city_id=a.city_id)
 -- Strong literal matching: exact category OR exact Place; fuzzy area alone
 -- is insufficient. This predicate is not a calibrated probability.
 AND ((coalesce(si.constraints->>'category','')<>'' AND si.constraints->>'category'=a.category_code)
      OR (coalesce(si.constraints->>'placeId','')<>'' AND si.constraints->>'placeId'=a.place_id::text))
 AND (coalesce(si.constraints->>'category','')='' OR si.constraints->>'category'=a.category_code)
 AND (coalesce(si.constraints->>'placeId','')='' OR si.constraints->>'placeId'=a.place_id::text)
 AND (coalesce(si.constraints->>'areaLabel','')='' OR strpos(lower(p.name),lower(si.constraints->>'areaLabel'))>0 OR strpos(lower(coalesce(p.address_label,'')),lower(si.constraints->>'areaLabel'))>0)
 AND a.publication_status='published' AND a.cancelled_at IS NULL AND isfinite(a.starts_at) AND isfinite(a.ends_at) AND a.ends_at>stamp.n AND (a.expires_at IS NULL OR (isfinite(a.expires_at) AND a.expires_at>stamp.n))
 AND birdtie_activity_visible_to(a.id,recipient)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=recipient AND bl.blocked_account_id=host.id) OR(bl.blocker_account_id=host.id AND bl.blocked_account_id=recipient))
 AND (co.id IS NULL OR (co.lifecycle_status='active' AND co.publication_status='published' AND community_owner.account_type='person' AND community_owner.status='active' AND (co.expires_at IS NULL OR (isfinite(co.expires_at) AND co.expires_at>stamp.n))))
 AND (org.id IS NULL OR (org.status='active' AND org_principal.account_type='organization' AND org_principal.status='active'))
 AND (biz.id IS NULL OR (biz.status='active' AND biz.claim_status='verified' AND biz_principal.account_type='business' AND biz_principal.status='active' AND EXISTS(SELECT 1 FROM business_venue_relations relation JOIN venues venue ON venue.place_id=relation.place_id JOIN venue_candidates candidate ON candidate.id=venue.source_candidate_id AND candidate.status='approved' AND isfinite(candidate.expires_at) AND candidate.expires_at>stamp.n WHERE relation.business_id=biz.id AND relation.place_id=p.id AND relation.status='verified' AND isfinite(venue.expires_at) AND venue.expires_at>stamp.n)))
 )
 SELECT q.actor,q.context,'activity:'||q.activity_revision::text,encode(sha256(convert_to(q.fingerprint::text,'UTF8')),'hex')
 FROM qualified q ORDER BY q.intent_created,q.intent_id LIMIT 1;
END $$;
CREATE OR REPLACE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record; current_policy record; expected_cat text;
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
    expected_cat:=CASE WHEN NEW.kind='opportunity_available' THEN 'ACTIVITY' WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY'
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
CREATE OR REPLACE FUNCTION birdtie_native_notification_visible(d native_notification_decisions) RETURNS boolean LANGUAGE sql VOLATILE AS $$
    SELECT d.disposition IN ('IMMEDIATE','NORMAL') AND EXISTS(
        SELECT 1 FROM birdtie_native_notification_source(d.kind,d.source_id,d.recipient_id) s
        CROSS JOIN birdtie_native_notification_preference(d.recipient_id,d.category) p
        WHERE s.actor_id=d.actor_id AND s.event_version=d.event_version AND s.source_version=d.source_version
          AND p.version=d.policy_version AND p.disposition=d.disposition AND p.priority=d.priority AND p.reason=d.reason);
$$;
COMMIT;
