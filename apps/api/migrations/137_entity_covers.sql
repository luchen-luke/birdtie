BEGIN;
CREATE TABLE entity_covers (
 entity_type text NOT NULL CHECK(entity_type IN('activity','organization')),
 entity_id uuid NOT NULL, version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 thumbnail bytea,sha256 text,rights jsonb,uploaded_by uuid REFERENCES accounts(id),
 retention_until timestamptz,attribution text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(entity_type,entity_id),
 CHECK((thumbnail IS NULL AND sha256 IS NULL AND rights IS NULL AND retention_until IS NULL)
 OR(thumbnail IS NOT NULL AND octet_length(thumbnail) BETWEEN 1 AND 262144 AND sha256 ~ '^[0-9a-f]{64}$'
 AND rights IS NOT NULL AND uploaded_by IS NOT NULL AND isfinite(retention_until) AND retention_until IS NOT NULL))
);
CREATE TABLE entity_cover_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,
 entity_type text NOT NULL CHECK(entity_type IN('activity','organization')),entity_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 applied_version bigint NOT NULL,action text NOT NULL,status text NOT NULL,
 input_sha256 text NOT NULL CHECK(input_sha256 ~ '^[0-9a-f]{64}$'),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='SUCCEEDED' AND action IN('SAVE','DELETE') AND applied_version=expected_version+1)
 OR(status='STOPPED' AND action='STOP' AND applied_version=0))
);
CREATE FUNCTION birdtie_entity_cover_manage(k text,i uuid,v uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM accounts person WHERE person.id=v AND person.account_type='person' AND person.status='active') AND CASE k
 WHEN 'organization' THEN EXISTS(SELECT 1 FROM organizations o JOIN accounts p ON p.id=o.account_id AND p.account_type='organization' AND p.status='active'
 JOIN organization_memberships m ON m.organization_id=o.id AND m.user_account_id=v AND m.status='active' AND m.role IN('owner','admin') WHERE o.id=i AND o.status='active')
 WHEN 'activity' THEN EXISTS(SELECT 1 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id
 LEFT JOIN communities c ON c.id=ao.community_id LEFT JOIN community_memberships cm ON cm.community_id=c.id AND cm.user_account_id=v AND cm.status='active' AND cm.role IN('owner','admin')
 LEFT JOIN organizations o ON o.id=ao.organization_id LEFT JOIN accounts op ON op.id=o.account_id AND op.account_type='organization' AND op.status='active'
 LEFT JOIN organization_memberships om ON om.organization_id=o.id AND om.user_account_id=v AND om.status='active' AND om.role IN('owner','admin')
 LEFT JOIN businesses b ON b.id=ao.business_id AND b.status='active' AND b.claim_status='verified' LEFT JOIN accounts bp ON bp.id=b.account_id AND bp.status='active'
 LEFT JOIN business_memberships bm ON bm.business_id=b.id AND bm.user_account_id=v AND bm.status='active' AND bm.role IN('owner','admin')
 WHERE a.id=i AND(ao.person_account_id=v OR(c.lifecycle_status='active' AND cm.id IS NOT NULL) OR(o.status='active' AND op.id IS NOT NULL AND om.id IS NOT NULL) OR(b.id IS NOT NULL AND bp.id IS NOT NULL AND bm.id IS NOT NULL))) ELSE false END
$$;
CREATE FUNCTION birdtie_entity_cover_visible(k text,i uuid,v uuid,at timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE k
 WHEN 'activity' THEN EXISTS(SELECT 1 FROM activities a JOIN cities city ON city.id=a.city_id WHERE a.id=i AND city.publication_status='published' AND(city.expires_at IS NULL OR city.expires_at>at) AND(a.expires_at IS NULL OR a.expires_at>at)
 AND (birdtie_entity_cover_manage(k,i,v) OR(a.publication_status='published' AND birdtie_activity_visible_to(a.id,v)))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE v IS NOT NULL AND((bl.blocker_account_id=v AND bl.blocked_account_id=a.host_account_id) OR(bl.blocked_account_id=v AND bl.blocker_account_id=a.host_account_id))))
 WHEN 'organization' THEN EXISTS(SELECT 1 FROM organizations o JOIN accounts p ON p.id=o.account_id AND p.account_type='organization' AND p.status='active'
 WHERE o.id=i AND o.status='active' AND(o.visibility='public' OR EXISTS(SELECT 1 FROM organization_memberships m WHERE m.organization_id=o.id AND m.user_account_id=v AND m.status='active'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE v IS NOT NULL AND((bl.blocker_account_id=v AND bl.blocked_account_id=o.account_id) OR(bl.blocked_account_id=v AND bl.blocker_account_id=o.account_id)))) ELSE false END
$$;
-- No lease clock is included in this source fingerprint. It includes resource,
-- organizer/role and block rows, including restore ABA and retained safety history.
CREATE FUNCTION birdtie_entity_cover_authority(k text,i uuid,v uuid) RETURNS text LANGUAGE sql STABLE AS $$
 WITH acts AS(SELECT a.id,a.host_account_id,a.city_id,a.xmin::text token FROM activities a WHERE k='activity' AND a.id=i),
 organizers AS(SELECT ao.*,ao.xmin::text token FROM activity_organizers ao WHERE ao.activity_id IN(SELECT id FROM acts)),
 orgs AS(SELECT o.*,o.xmin::text token FROM organizations o WHERE(k='organization' AND o.id=i) OR o.id IN(SELECT organization_id FROM organizers)),
 comms AS(SELECT c.*,c.xmin::text token FROM communities c WHERE c.id IN(SELECT community_id FROM organizers)),
 biz AS(SELECT b.*,b.xmin::text token FROM businesses b WHERE b.id IN(SELECT business_id FROM organizers)),
 people AS(SELECT v id UNION SELECT host_account_id FROM acts UNION SELECT account_id FROM orgs UNION SELECT owner_account_id FROM comms UNION SELECT account_id FROM biz UNION SELECT person_account_id FROM organizers
 UNION SELECT user_account_id FROM organization_memberships WHERE organization_id IN(SELECT id FROM orgs)
 UNION SELECT user_account_id FROM community_memberships WHERE community_id IN(SELECT id FROM comms)
 UNION SELECT user_account_id FROM business_memberships WHERE business_id IN(SELECT id FROM biz)
 UNION SELECT uploaded_by FROM entity_covers WHERE entity_type=k AND entity_id=i),
 tokens AS(
 SELECT 'activity' t,id::text id,token FROM acts UNION ALL SELECT 'organizer',activity_id::text,token FROM organizers
 UNION ALL SELECT 'organization',id::text,token FROM orgs UNION ALL SELECT 'community',id::text,token FROM comms UNION ALL SELECT 'business',id::text,token FROM biz
 UNION ALL SELECT 'city',c.id,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM acts)
 UNION ALL SELECT 'person',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT id FROM people)
 UNION ALL SELECT 'viewer-session',se.id::text,se.authorization_generation::text FROM sessions se WHERE v IS NOT NULL AND se.account_id=v
 UNION ALL SELECT 'org-member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT id FROM orgs)
 UNION ALL SELECT 'community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT id FROM comms)
 UNION ALL SELECT 'business-member',m.id::text,m.xmin::text FROM business_memberships m WHERE m.business_id IN(SELECT id FROM biz)
 UNION ALL SELECT 'block',concat_ws(':',b.blocker_account_id,b.blocked_account_id),b.xmin::text FROM account_blocks b WHERE b.blocker_account_id IN(SELECT id FROM people) OR b.blocked_account_id IN(SELECT id FROM people)
 UNION ALL SELECT 'safety',e.id::text,e.action FROM audit_events e WHERE e.purpose='personal_safety' AND(e.actor_account_id IN(SELECT id FROM people) OR e.resource_id IN(SELECT id::text FROM people)))
 SELECT encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(t,id,token) ORDER BY t,id),'[]'::jsonb)::text,'UTF8')),'hex') FROM tokens
$$;
CREATE FUNCTION birdtie_entity_cover_metadata(k text,i uuid,v uuid,at timestamptz DEFAULT clock_timestamp()) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('entityType',k,'entityId',i,'version',c.version,'thumbnailPath','/v1/'||CASE k WHEN 'activity' THEN 'activities' ELSE 'organizations' END||'/'||i::text||'/cover/'||c.version::text||'/content',
 'width',320,'height',180,'byteCount',octet_length(c.thumbnail),'sha256',c.sha256,'expiresAt',least(at+interval '60 seconds',c.retention_until,
 (SELECT a.expires_at FROM activities a WHERE k='activity' AND a.id=i),(SELECT city.expires_at FROM cities city JOIN activities a ON a.city_id=city.id WHERE k='activity' AND a.id=i)),'attribution',c.attribution)
 FROM entity_covers c WHERE c.entity_type=k AND c.entity_id=i AND c.thumbnail IS NOT NULL AND c.retention_until>at
 AND birdtie_entity_cover_manage(k,i,c.uploaded_by) AND birdtie_entity_cover_visible(k,i,v,at)
$$;
CREATE FUNCTION birdtie_entity_cover_operation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'cover operation history is immutable' USING ERRCODE='23514'; END IF;
 IF NOT birdtie_entity_cover_manage(NEW.entity_type,NEW.entity_id,NEW.actor_account_id) THEN RAISE EXCEPTION 'cover operation needs current manager' USING ERRCODE='23514'; END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM entity_covers c WHERE c.entity_type=NEW.entity_type AND c.entity_id=NEW.entity_id AND c.version=NEW.applied_version)
 THEN RAISE EXCEPTION 'cover receipt must identify actual effect' USING ERRCODE='23514'; END IF;
 RETURN NEW;
 END $$;
CREATE TRIGGER entity_cover_operations_immutable BEFORE INSERT OR UPDATE OR DELETE ON entity_cover_operations FOR EACH ROW EXECUTE FUNCTION birdtie_entity_cover_operation_immutable();
COMMIT;
