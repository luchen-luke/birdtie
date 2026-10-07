BEGIN;

-- Explicit human notification preferences. This is not enrichment consent,
-- a model egress grant, an action approval or an Agent processing capability.
CREATE TABLE native_notification_policies (
    owner_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version bigint NOT NULL CHECK(version>0),
    enabled boolean NOT NULL,
    default_route text NOT NULL CHECK(default_route IN ('IMMEDIATE','NORMAL','DIGEST','SILENT','BLOCK')),
    rules jsonb NOT NULL CHECK(jsonb_typeof(rules)='array' AND jsonb_array_length(rules)<=8),
    pause_until timestamptz,
    expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(updated_at)),
    CHECK(pause_until IS NULL OR (isfinite(pause_until) AND pause_until<=expires_at)),
    CHECK(expires_at>updated_at AND expires_at<=updated_at+interval '720 hours')
);

CREATE FUNCTION birdtie_native_notification_policy_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rule jsonb; seen text[]:='{}';
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
        JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
        WHERE a.id=NEW.owner_id AND a.account_type='person' AND a.status='active'
          AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active') THEN
        RAISE EXCEPTION 'notification policy owner unavailable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND (NEW.owner_id<>OLD.owner_id OR NEW.agent_id<>OLD.agent_id
        OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1) THEN
        RAISE EXCEPTION 'notification policy binding/version immutable' USING ERRCODE='23514';
    ELSIF TG_OP='INSERT' AND NEW.version<>1 THEN
        RAISE EXCEPTION 'notification policy initial version invalid' USING ERRCODE='23514';
    END IF;
    FOR rule IN SELECT value FROM jsonb_array_elements(NEW.rules) LOOP
        IF jsonb_typeof(rule) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(rule))<>2
           OR jsonb_typeof(rule->'category') IS DISTINCT FROM 'string'
           OR jsonb_typeof(rule->'route') IS DISTINCT FROM 'string'
           OR (rule->>'category') NOT IN ('MESSAGE','ACTIVITY','COMMUNITY','ORGANIZATION','BUSINESS','SYSTEM','AGENT','SOCIAL')
           OR (rule->>'route') NOT IN ('IMMEDIATE','NORMAL','DIGEST','SILENT','BLOCK')
           OR rule->>'category'=ANY(seen) THEN
            RAISE EXCEPTION 'notification policy rule invalid' USING ERRCODE='23514';
        END IF;
        seen:=array_append(seen,rule->>'category');
    END LOOP;
    NEW.updated_at:=clock_timestamp();
    IF NEW.expires_at<=NEW.updated_at OR NEW.expires_at>NEW.updated_at+interval '720 hours' THEN
        RAISE EXCEPTION 'notification policy expiry invalid' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER native_notification_policy_guard BEFORE INSERT OR UPDATE ON native_notification_policies
    FOR EACH ROW EXECUTE FUNCTION birdtie_native_notification_policy_guard();

-- Metadata only. Real event identity and current authorization fingerprint are
-- distinct: policy/source changes never manufacture a second event delivery.
CREATE TABLE native_notification_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK(kind IN ('activity_reminder','activity_change','activity_cancelled','activity_review',
        'place_review','direct_message','connection_request','connection_decision','community_message',
        'activity_message','organization_invitation','organization_membership_change','agent_task_completed','agent_task_failed')),
    category text NOT NULL CHECK(category IN ('MESSAGE','ACTIVITY','COMMUNITY','ORGANIZATION','BUSINESS','SYSTEM','AGENT','SOCIAL')),
    source_id uuid NOT NULL,
    event_version text NOT NULL CHECK(length(event_version) BETWEEN 1 AND 80),
    source_version text NOT NULL CHECK(source_version~'^[0-9a-f]{64}$'),
    policy_version bigint NOT NULL CHECK(policy_version>=0),
    disposition text NOT NULL CHECK(disposition IN ('IMMEDIATE','NORMAL','DIGEST','SILENT','BLOCK')),
    priority smallint NOT NULL CHECK(priority IN (0,10,50,100)),
    reason text NOT NULL CHECK(reason IN ('not_configured','disabled','expired','exact_rule','default_rule','attention_paused')),
    activity_id uuid REFERENCES activities(id) ON DELETE CASCADE,
    activity_candidate_id uuid REFERENCES activity_candidates(id) ON DELETE CASCADE,
    place_candidate_id uuid REFERENCES place_candidates(id) ON DELETE CASCADE,
    message_id uuid REFERENCES conversation_messages(id) ON DELETE CASCADE,
    connection_request_id uuid REFERENCES connection_requests(id) ON DELETE CASCADE,
    community_message_id uuid REFERENCES community_conversation_messages(id) ON DELETE CASCADE,
    activity_message_id uuid REFERENCES activity_conversation_messages(id) ON DELETE CASCADE,
    organization_membership_id uuid REFERENCES organization_memberships(id) ON DELETE CASCADE,
    agent_task_id uuid REFERENCES agent_tasks(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
    CHECK(num_nonnulls(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,
        community_message_id,activity_message_id,organization_membership_id,agent_task_id)=1),
    CHECK(source_id=COALESCE(activity_id,activity_candidate_id,place_candidate_id,message_id,connection_request_id,
        community_message_id,activity_message_id,organization_membership_id,agent_task_id)),
    CHECK((kind IN ('activity_reminder','activity_change','activity_cancelled'))=(activity_id IS NOT NULL)),
    CHECK((kind='activity_review')=(activity_candidate_id IS NOT NULL)),
    CHECK((kind='place_review')=(place_candidate_id IS NOT NULL)),
    CHECK((kind='direct_message')=(message_id IS NOT NULL)),
    CHECK((kind IN ('connection_request','connection_decision'))=(connection_request_id IS NOT NULL)),
    CHECK((kind='community_message')=(community_message_id IS NOT NULL)),
    CHECK((kind='activity_message')=(activity_message_id IS NOT NULL)),
    CHECK((kind IN ('organization_invitation','organization_membership_change'))=(organization_membership_id IS NOT NULL)),
    CHECK((kind IN ('agent_task_completed','agent_task_failed'))=(agent_task_id IS NOT NULL)),
    CHECK(priority=CASE disposition WHEN 'IMMEDIATE' THEN 100 WHEN 'NORMAL' THEN 50 WHEN 'DIGEST' THEN 10 ELSE 0 END),
    UNIQUE(recipient_id,kind,source_id,event_version)
);
CREATE INDEX native_notification_pending_digest ON native_notification_decisions(recipient_id,created_at,id)
    WHERE disposition='DIGEST';
ALTER TABLE inbox_items ADD COLUMN routing_decision_id uuid UNIQUE
    REFERENCES native_notification_decisions(id) ON DELETE CASCADE;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;
ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN (
    'place_candidate','activity_candidate','community','connection_request','conversation_message',
    'activity_change','activity_cancelled','activity_reminder','community_message','activity_message',
    'organization_membership','agent_task'));

CREATE FUNCTION birdtie_native_notification_activity_chat_allowed(activity uuid,person uuid)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM activities a JOIN cities c ON c.id=a.city_id
 JOIN accounts host ON host.id=a.host_account_id AND host.status='active'
 JOIN accounts human ON human.id=person AND human.account_type='person' AND human.status='active'
 JOIN activity_organizers ao ON ao.activity_id=a.id
 LEFT JOIN communities co ON co.id=ao.community_id
 LEFT JOIN organizations o ON o.id=ao.organization_id
 LEFT JOIN businesses b ON b.id=ao.business_id
 WHERE a.id=activity AND a.publication_status='published' AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp()
  AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,person)
  AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=person AND bl.blocked_account_id=host.id)
       OR (bl.blocker_account_id=host.id AND bl.blocked_account_id=person))
  AND (co.id IS NULL OR (co.lifecycle_status='active' AND co.publication_status='published'
       AND (co.expires_at IS NULL OR co.expires_at>clock_timestamp())))
  AND (o.id IS NULL OR o.status='active') AND (b.id IS NULL OR (b.status='active' AND b.claim_status='verified'))
  AND (EXISTS(SELECT 1 FROM activity_participations p WHERE p.activity_id=a.id AND p.participant_account_id=person AND p.status='going')
    OR ao.person_account_id=person
    OR EXISTS(SELECT 1 FROM community_memberships cm WHERE cm.community_id=ao.community_id AND cm.user_account_id=person
        AND cm.status='active' AND cm.role IN ('owner','admin'))
    OR EXISTS(SELECT 1 FROM organization_memberships om WHERE om.organization_id=ao.organization_id AND om.user_account_id=person
        AND om.status='active' AND om.role IN ('owner','admin'))
    OR EXISTS(SELECT 1 FROM business_memberships bm WHERE bm.business_id=ao.business_id AND bm.user_account_id=person
        AND bm.status='active' AND bm.role IN ('owner','admin'))));
$$;

-- Ordinary notification current-source resolver. It never reads messages,
-- query, profile values or human-authored titles. xmin is an opaque comparison
-- token for current retained rows; it is not a historic CAS revision/grant.
CREATE FUNCTION birdtie_native_notification_source(k text,sid uuid,recipient uuid)
RETURNS TABLE(actor_id uuid,context_id uuid,event_version text,source_version text)
LANGUAGE plpgsql VOLATILE AS $$
DECLARE actor uuid; context uuid; event_v text; versions jsonb;
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=recipient AND account_type='person' AND status='active') THEN RETURN; END IF;
    IF k IN ('activity_reminder','activity_change','activity_cancelled') THEN
        SELECT a.host_account_id,a.id,a.xmin::text,
            jsonb_build_array(a.xmin::text,p.xmin::text,c.xmin::text,h.xmin::text,ao.xmin::text,
                co.xmin::text,o.xmin::text,b.xmin::text)
        INTO actor,context,event_v,versions
        FROM activities a JOIN activity_participations p ON p.activity_id=a.id AND p.participant_account_id=recipient
        JOIN cities c ON c.id=a.city_id JOIN accounts h ON h.id=a.host_account_id
        JOIN activity_organizers ao ON ao.activity_id=a.id
        LEFT JOIN communities co ON co.id=ao.community_id
        LEFT JOIN organizations o ON o.id=ao.organization_id
        LEFT JOIN businesses b ON b.id=ao.business_id
        WHERE a.id=sid AND p.status IN ('going','pending') AND a.publication_status='published'
          AND c.publication_status='published' AND h.status='active'
          AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
          AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
          AND birdtie_activity_visible_to(a.id,recipient)
          AND (co.id IS NULL OR (co.publication_status='published' AND co.lifecycle_status='active'
              AND (co.expires_at IS NULL OR co.expires_at>clock_timestamp())))
          AND (o.id IS NULL OR o.status='active')
          AND (b.id IS NULL OR (b.status='active' AND b.claim_status='verified'))
          AND ((k='activity_cancelled' AND a.cancelled_at IS NOT NULL) OR
              (k<>'activity_cancelled' AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp()))
          AND (k<>'activity_reminder' OR (a.starts_at>clock_timestamp() AND a.starts_at<=clock_timestamp()+interval '2 hours'));
    ELSIF k IN ('connection_request','connection_decision') THEN
        SELECT CASE WHEN k='connection_request' THEN r.sender_account_id ELSE r.recipient_account_id END,
            r.id,r.xmin::text,jsonb_build_array(r.xmin::text)
        INTO actor,context,event_v,versions FROM connection_requests r WHERE r.id=sid
          AND ((k='connection_request' AND r.recipient_account_id=recipient AND r.state='pending' AND r.expires_at>clock_timestamp())
            OR (k='connection_decision' AND r.sender_account_id=recipient AND r.state IN ('accepted','declined')));
    ELSIF k='direct_message' THEN
        SELECT m.sender_account_id,c.id,m.xmin::text,
            jsonb_build_array(m.xmin::text,c.xmin::text,r.xmin::text,t.xmin::text)
        INTO actor,context,event_v,versions
        FROM conversation_messages m JOIN conversations c ON c.id=m.conversation_id
        JOIN connection_requests r ON r.id=c.request_id
        LEFT JOIN person_ties t ON t.person_a_account_id=LEAST(c.member_a_account_id,c.member_b_account_id)
          AND t.person_b_account_id=GREATEST(c.member_a_account_id,c.member_b_account_id) AND t.status='active'
        WHERE m.id=sid AND m.speaker_kind='human' AND m.sender_account_id<>recipient
          AND recipient IN (c.member_a_account_id,c.member_b_account_id) AND r.state='accepted'
          AND (r.scope<>'friend' OR t.status='active');
    ELSIF k='community_message' THEN
        SELECT m.sender_account_id,cv.id,m.xmin::text,
            jsonb_build_array(m.xmin::text,co.xmin::text,me.xmin::text,sender.xmin::text,cm.xmin::text,sm.xmin::text)
        INTO actor,context,event_v,versions
        FROM community_conversation_messages m JOIN community_conversations cv ON cv.id=m.conversation_id
        JOIN communities co ON co.id=cv.community_id
        JOIN community_conversation_members me ON me.conversation_id=cv.id AND me.account_id=recipient AND me.status='active'
        JOIN community_conversation_members sender ON sender.conversation_id=cv.id AND sender.account_id=m.sender_account_id AND sender.status='active'
        JOIN community_memberships cm ON cm.community_id=co.id AND cm.user_account_id=recipient AND cm.status='active'
        JOIN community_memberships sm ON sm.community_id=co.id AND sm.user_account_id=m.sender_account_id AND sm.status='active'
        WHERE m.id=sid AND m.sender_account_id<>recipient AND m.removed_at IS NULL
          AND co.lifecycle_status='active' AND co.publication_status='published' AND co.visibility<>'hidden'
          AND (co.expires_at IS NULL OR co.expires_at>clock_timestamp())
          AND EXISTS(SELECT 1 FROM accounts own WHERE own.id=co.owner_account_id AND own.status='active')
          AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE
              (bl.blocker_account_id=recipient AND bl.blocked_account_id=co.owner_account_id)
              OR (bl.blocker_account_id=co.owner_account_id AND bl.blocked_account_id=recipient));
    ELSIF k='activity_message' THEN
        SELECT m.sender_account_id,cv.id,m.xmin::text,
            jsonb_build_array(m.xmin::text,a.xmin::text,c.xmin::text,me.xmin::text,sender.xmin::text,p.xmin::text,sp.xmin::text)
        INTO actor,context,event_v,versions
        FROM activity_conversation_messages m JOIN activity_conversations cv ON cv.id=m.conversation_id
        JOIN activities a ON a.id=cv.activity_id JOIN cities c ON c.id=a.city_id
        JOIN activity_conversation_members me ON me.conversation_id=cv.id AND me.account_id=recipient AND me.status='active'
        JOIN activity_conversation_members sender ON sender.conversation_id=cv.id AND sender.account_id=m.sender_account_id AND sender.status='active'
        LEFT JOIN activity_participations p ON p.activity_id=a.id AND p.participant_account_id=recipient AND p.status='going'
        LEFT JOIN activity_participations sp ON sp.activity_id=a.id AND sp.participant_account_id=m.sender_account_id AND sp.status='going'
        WHERE m.id=sid AND m.sender_account_id<>recipient AND m.removed_at IS NULL
          AND a.publication_status='published' AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp()
          AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
          AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
          AND birdtie_native_notification_activity_chat_allowed(a.id,recipient)
          AND birdtie_native_notification_activity_chat_allowed(a.id,m.sender_account_id);
    ELSIF k IN ('organization_invitation','organization_membership_change') THEN
        SELECT o.account_id,o.id,m.xmin::text,jsonb_build_array(m.xmin::text,o.xmin::text)
        INTO actor,context,event_v,versions FROM organization_memberships m
        JOIN organizations o ON o.id=m.organization_id WHERE m.id=sid AND m.user_account_id=recipient
          AND o.status='active' AND ((k='organization_invitation' AND m.status='invited')
            OR (k='organization_membership_change' AND m.status IN ('active','removed')));
    ELSIF k IN ('agent_task_completed','agent_task_failed') THEN
        -- One terminal status notice per native Task, even if its ancillary
        -- conversation is edited. Such an edit invalidates the retained source
        -- fingerprint; it is not a new Run or permission to replay a notice.
        SELECT t.owner_account_id,t.id,t.status,jsonb_build_array(t.xmin::text,ag.xmin::text,ap.xmin::text)
        INTO actor,context,event_v,versions FROM agent_tasks t
        JOIN agents ag ON ag.principal_account_id=t.owner_account_id AND ag.agent_type='personal' AND ag.status='active'
        JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=recipient AND ap.owner_type='PERSON'
        WHERE t.id=sid AND t.owner_account_id=recipient AND t.principal_type='person'
          AND ((k='agent_task_completed' AND t.status='COMPLETED') OR (k='agent_task_failed' AND t.status='FAILED'));
    ELSIF k='activity_review' THEN
        SELECT ac.reviewed_by,ac.id,ac.xmin::text,jsonb_build_array(ac.xmin::text,c.xmin::text)
        INTO actor,context,event_v,versions FROM activity_candidates ac JOIN cities c ON c.id=ac.city_id
        WHERE ac.id=sid AND ac.submitted_by=recipient AND ac.status IN ('published','rejected')
          AND ac.reviewed_by IS NOT NULL AND c.publication_status='published'
          AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp());
    ELSIF k='place_review' THEN
        SELECT pc.reviewed_by,pc.id,pc.xmin::text,jsonb_build_array(pc.xmin::text,c.xmin::text)
        INTO actor,context,event_v,versions FROM place_candidates pc JOIN cities c ON c.id=pc.city_id
        WHERE pc.id=sid AND pc.submitted_by=recipient AND pc.status IN ('published','linked_duplicate','rejected')
          AND pc.reviewed_by IS NOT NULL AND c.publication_status='published'
          AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp());
    ELSE RETURN;
    END IF;
    IF actor IS NULL OR NOT EXISTS(SELECT 1 FROM accounts WHERE id=actor AND status='active')
       OR EXISTS(SELECT 1 FROM account_blocks WHERE (blocker_account_id=recipient AND blocked_account_id=actor)
            OR (blocker_account_id=actor AND blocked_account_id=recipient)) THEN RETURN; END IF;
    RETURN QUERY SELECT actor,context,event_v,encode(sha256(convert_to(versions::text,'UTF8')),'hex');
END $$;

CREATE FUNCTION birdtie_native_notification_preference(recipient uuid,cat text)
RETURNS TABLE(version bigint,disposition text,priority smallint,reason text)
LANGUAGE plpgsql VOLATILE AS $$
DECLARE p native_notification_policies; selected text; why text; observed timestamptz:=clock_timestamp();
BEGIN
    IF cat NOT IN ('MESSAGE','ACTIVITY','COMMUNITY','ORGANIZATION','BUSINESS','SYSTEM','AGENT','SOCIAL') THEN RETURN; END IF;
    SELECT np.* INTO p FROM native_notification_policies np WHERE np.owner_id=recipient;
    IF NOT FOUND THEN selected:='NORMAL';why:='not_configured';
    ELSE
      IF NOT EXISTS(SELECT 1 FROM agents ag JOIN agent_profiles ap ON ap.agent_id=ag.id
          AND ap.owner_id=recipient AND ap.owner_type='PERSON' WHERE ag.id=p.agent_id
          AND ag.principal_account_id=recipient AND ag.agent_type='personal' AND ag.status='active') THEN RETURN;END IF;
      IF NOT p.enabled THEN selected:='NORMAL';why:='disabled';
    ELSIF p.expires_at<=observed THEN selected:='NORMAL';why:='expired';
    ELSE
        SELECT value->>'route' INTO selected FROM jsonb_array_elements(p.rules) WHERE value->>'category'=cat;
        IF selected IS NULL THEN selected:=p.default_route;why:='default_rule';ELSE why:='exact_rule';END IF;
        IF selected<>'BLOCK' AND p.pause_until>observed THEN selected:='SILENT';why:='attention_paused';END IF;
      END IF;
    END IF;
    RETURN QUERY SELECT COALESCE(p.version,0),selected,
        (CASE selected WHEN 'IMMEDIATE' THEN 100 WHEN 'NORMAL' THEN 50 WHEN 'DIGEST' THEN 10 ELSE 0 END)::smallint,why;
END $$;

CREATE FUNCTION birdtie_native_notification_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_source record; current_policy record; expected_cat text;
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'notification decision immutable' USING ERRCODE='23514';END IF;
    expected_cat:=CASE WHEN NEW.kind LIKE 'activity_%' AND NEW.kind<>'activity_message' THEN 'ACTIVITY'
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
CREATE TRIGGER native_notification_decision_guard BEFORE INSERT OR UPDATE ON native_notification_decisions
    FOR EACH ROW EXECUTE FUNCTION birdtie_native_notification_decision_guard();

CREATE FUNCTION birdtie_native_notification_visible(d native_notification_decisions) RETURNS boolean LANGUAGE sql VOLATILE AS $$
    SELECT d.disposition IN ('IMMEDIATE','NORMAL') AND EXISTS(
        SELECT 1 FROM birdtie_native_notification_source(d.kind,d.source_id,d.recipient_id) s
        CROSS JOIN birdtie_native_notification_preference(d.recipient_id,d.category) p
        WHERE s.actor_id=d.actor_id AND s.event_version=d.event_version AND s.source_version=d.source_version
          AND p.version=d.policy_version AND p.disposition=d.disposition AND p.priority=d.priority AND p.reason=d.reason);
$$;
COMMIT;
