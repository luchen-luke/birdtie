BEGIN;

-- This permission is deliberately separate from merchant claim/profile/venue
-- certification and from ordinary City editing. Trusted local provisioning is
-- required; there is no client self-grant endpoint or financial assertion.
CREATE TABLE sponsored_opportunity_review_grants (
 city_id text NOT NULL REFERENCES cities(id),
 reviewer_account_id uuid NOT NULL REFERENCES accounts(id),
 permission text NOT NULL CHECK(permission='review_sponsorship'),
 state text NOT NULL CHECK(state IN ('active','revoked')),
 valid_from timestamptz NOT NULL,
 valid_until timestamptz NOT NULL,
 provisioned_by uuid NOT NULL REFERENCES accounts(id),
 provision_note text NOT NULL CHECK(length(btrim(provision_note)) BETWEEN 1 AND 2000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(city_id,reviewer_account_id),
 CHECK(provisioned_by<>reviewer_account_id),
 CHECK(isfinite(valid_from) AND isfinite(valid_until) AND valid_until>valid_from AND valid_until<=valid_from+interval '30 days')
);

CREATE TABLE sponsored_opportunity_declarations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 business_id uuid NOT NULL REFERENCES businesses(id),
 city_id text NOT NULL REFERENCES cities(id),
 activity_id uuid REFERENCES activities(id),
 place_id uuid REFERENCES places(id),
 submitted_by uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 source_snapshot text NOT NULL CHECK(source_snapshot ~ '^[0-9a-f]{64}$'),
 statement text NOT NULL CHECK(length(btrim(statement)) BETWEEN 1 AND 1000 AND statement=btrim(statement) AND statement!~'[[:cntrl:]]'),
 source_url text NOT NULL CHECK(birdtie_business_https_valid(source_url)),
 rights_note text NOT NULL CHECK(length(btrim(rights_note)) BETWEEN 1 AND 2000 AND rights_note=btrim(rights_note) AND rights_note!~'[[:cntrl:]]'),
 observed_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 9007199254740991),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected','revoked')),
 reviewed_by uuid REFERENCES accounts(id),
 reviewed_at timestamptz,
 review_note text NOT NULL DEFAULT '' CHECK(length(review_note)<=2000 AND review_note!~'[[:cntrl:]]'),
 review_generation text CHECK(review_generation ~ '^[0-9a-f]{64}$'),
 review_request_hash bytea CHECK(octet_length(review_request_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(business_id,submitted_by,operation_id),
 CHECK(num_nonnulls(activity_id,place_id)=1),
 CHECK(isfinite(observed_at) AND isfinite(expires_at) AND isfinite(created_at) AND observed_at<=created_at AND expires_at>created_at AND expires_at<=observed_at+interval '30 days'),
 CHECK((status='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note='' AND review_generation IS NULL AND review_request_hash IS NULL) OR
       (status IN ('approved','rejected') AND reviewed_by IS NOT NULL AND reviewed_by<>submitted_by AND reviewed_at IS NOT NULL AND isfinite(reviewed_at) AND reviewed_at>=observed_at AND length(btrim(review_note))>0 AND review_generation IS NOT NULL AND review_request_hash IS NOT NULL) OR
       (status='revoked' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND isfinite(reviewed_at) AND length(btrim(review_note))>0 AND review_generation IS NOT NULL AND review_request_hash IS NOT NULL))
);
CREATE INDEX sponsored_opportunity_current_activity ON sponsored_opportunity_declarations(activity_id,created_at DESC,id) WHERE status='approved';
CREATE INDEX sponsored_opportunity_current_place ON sponsored_opportunity_declarations(place_id,created_at DESC,id) WHERE status='approved';

CREATE FUNCTION birdtie_sponsor_person_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_id uuid; other_id uuid;
BEGIN
 IF TG_TABLE_NAME='sponsored_opportunity_review_grants' THEN
  owner_id:=NEW.reviewer_account_id;other_id:=NEW.provisioned_by;
  IF TG_OP='UPDATE' AND (NEW.city_id IS DISTINCT FROM OLD.city_id OR NEW.reviewer_account_id IS DISTINCT FROM OLD.reviewer_account_id OR NEW.provisioned_by IS DISTINCT FROM OLD.provisioned_by OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN RAISE EXCEPTION 'stable sponsorship grant bindings required'; END IF;
 ELSE owner_id:=NEW.submitted_by;other_id:=NEW.reviewed_by; END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=owner_id AND account_type='person') OR
    (other_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=other_id AND account_type='person')) THEN RAISE EXCEPTION 'sponsorship requires Person bindings'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER sponsored_grant_person BEFORE INSERT OR UPDATE ON sponsored_opportunity_review_grants FOR EACH ROW EXECUTE FUNCTION birdtie_sponsor_person_guard();
CREATE TRIGGER sponsored_declaration_person BEFORE INSERT OR UPDATE ON sponsored_opportunity_declarations FOR EACH ROW EXECUTE FUNCTION birdtie_sponsor_person_guard();

CREATE FUNCTION birdtie_sponsor_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.revision<>1 OR NEW.status<>'pending' THEN RAISE EXCEPTION 'sponsorship starts pending at revision one'; END IF;
 ELSE
  IF (NEW.id,NEW.business_id,NEW.city_id,NEW.activity_id,NEW.place_id,NEW.submitted_by,NEW.operation_id,NEW.request_hash,NEW.source_snapshot,NEW.statement,NEW.source_url,NEW.rights_note,NEW.observed_at,NEW.expires_at,NEW.created_at)
     IS DISTINCT FROM (OLD.id,OLD.business_id,OLD.city_id,OLD.activity_id,OLD.place_id,OLD.submitted_by,OLD.operation_id,OLD.request_hash,OLD.source_snapshot,OLD.statement,OLD.source_url,OLD.rights_note,OLD.observed_at,OLD.expires_at,OLD.created_at) THEN RAISE EXCEPTION 'immutable sponsorship declaration'; END IF;
  IF OLD.revision=9007199254740991 OR NEW.revision<>OLD.revision+1 OR
     NOT ((OLD.status='pending' AND NEW.status IN ('approved','rejected','revoked')) OR (OLD.status='approved' AND NEW.status='revoked')) THEN RAISE EXCEPTION 'sponsorship transition CAS required'; END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER sponsored_declaration_version BEFORE INSERT OR UPDATE ON sponsored_opportunity_declarations FOR EACH ROW EXECUTE FUNCTION birdtie_sponsor_version_guard();

-- Opaque epoch snapshot of retained current facts; xmin is not a monotonic
-- counter. It invalidates ABA restore. Session last_seen is deliberately absent.
CREATE FUNCTION birdtie_sponsor_source_snapshot(bid uuid,person uuid,kind text,target uuid,at timestamptz)
RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object(
  'business',to_jsonb(b),'businessEpoch',b.xmin::text,'principalEpoch',bp.xmin::text,
  'claim',to_jsonb(cl),'claimEpoch',cl.xmin::text,'claimPersonEpoch',cp.xmin::text,'claimMembershipEpoch',cm.xmin::text,'claimReviewerEpoch',cr.xmin::text,'claimGrantEpoch',cg.xmin::text,
  'submitterEpoch',a.xmin::text,'membership',to_jsonb(m),'membershipEpoch',m.xmin::text,
  'city',to_jsonb(c),'cityEpoch',c.xmin::text,'target',src.facts
 )::text,'UTF8')),'hex')
 FROM businesses b JOIN accounts bp ON bp.id=b.account_id
 JOIN business_claim_controls cl ON cl.business_id=b.id
 JOIN accounts cp ON cp.id=cl.submitted_by
 JOIN business_memberships cm ON cm.business_id=b.id AND cm.user_account_id=cp.id
 JOIN accounts cr ON cr.id=cl.reviewed_by
 JOIN business_review_grants cg ON cg.business_id=b.id AND cg.reviewer_account_id=cr.id
 JOIN accounts a ON a.id=person JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=a.id
 JOIN LATERAL (
  SELECT act.city_id,jsonb_build_object('row',to_jsonb(act),'epoch',act.xmin::text,'organizer',to_jsonb(o),'organizerEpoch',o.xmin::text,'venue',venue.facts) facts
  FROM activities act JOIN activity_organizers o ON o.activity_id=act.id AND o.business_id=b.id
  LEFT JOIN LATERAL (
   SELECT jsonb_build_object('place',to_jsonb(p),'placeEpoch',p.xmin::text,'venue',to_jsonb(v),'venueEpoch',v.xmin::text,'candidate',to_jsonb(vc),'candidateEpoch',vc.xmin::text,'relationship',to_jsonb(vr),'relationshipEpoch',vr.xmin::text,'console',to_jsonb(vf),'consoleEpoch',vf.xmin::text,'reviewerEpoch',vra.xmin::text,'grantEpoch',vg.xmin::text,'venueSubmitterEpoch',vsa.xmin::text,'venueMembershipEpoch',vsm.xmin::text,'operator',to_jsonb(vo),'operatorEpoch',vo.xmin::text,'operatorPrincipalEpoch',va.xmin::text) facts
   FROM places p JOIN venues v ON v.place_id=p.id AND v.city_id=p.city_id JOIN venue_candidates vc ON vc.id=v.source_candidate_id
   JOIN business_venue_relations vr ON vr.place_id=p.id AND vr.business_id=b.id
   JOIN business_console_venue_facts vf ON vf.place_id=p.id AND vf.business_id=b.id
   JOIN accounts vra ON vra.id=vf.reviewed_by JOIN business_review_grants vg ON vg.business_id=b.id AND vg.reviewer_account_id=vra.id
   JOIN accounts vsa ON vsa.id=vf.submitted_by JOIN business_memberships vsm ON vsm.business_id=b.id AND vsm.user_account_id=vsa.id
   LEFT JOIN organizations vo ON vo.id=v.operator_organization_id LEFT JOIN accounts va ON va.id=vo.account_id
   WHERE p.id=act.place_id AND p.publication_status='published' AND p.updated_at<=at AND (p.expires_at IS NULL OR p.expires_at>at)
    AND v.expires_at>at AND vc.status='approved' AND vc.place_id=p.id AND vc.city_id=p.city_id AND vc.reviewed_by=v.reviewed_by AND (v.operator_organization_id IS NULL OR (vo.status='active' AND va.status='active' AND va.account_type='organization')) AND vsa.status='active' AND vsa.account_type='person' AND vsm.status='active' AND vsm.role IN ('owner','admin') AND vr.status='verified' AND vf.state='verified' AND vf.valid_until>at AND vf.updated_at<=at
    AND vra.status='active' AND vra.account_type='person' AND vg.state='active' AND 'venue'=ANY(vg.permissions) AND vg.valid_from<=at AND vg.valid_until>at
    AND NOT EXISTS(SELECT 1 FROM business_memberships mm WHERE mm.business_id=b.id AND mm.user_account_id=vra.id AND mm.status='active')
  ) venue ON true
  WHERE kind='ACTIVITY' AND act.id=target AND act.host_account_id=b.account_id AND act.publication_status='published' AND act.visibility='public'
   AND act.cancelled_at IS NULL AND act.ends_at>at AND act.updated_at<=at AND (act.expires_at IS NULL OR act.expires_at>at) AND (act.place_id IS NULL OR venue.facts IS NOT NULL)
  UNION ALL
  SELECT p.city_id,jsonb_build_object('place',to_jsonb(p),'placeEpoch',p.xmin::text,'venue',to_jsonb(v),'venueEpoch',v.xmin::text,'candidate',to_jsonb(vc),'candidateEpoch',vc.xmin::text,'relationship',to_jsonb(vr),'relationshipEpoch',vr.xmin::text,'console',to_jsonb(vf),'consoleEpoch',vf.xmin::text,'reviewerEpoch',vra.xmin::text,'grantEpoch',vg.xmin::text,'venueSubmitterEpoch',vsa.xmin::text,'venueMembershipEpoch',vsm.xmin::text,'operator',to_jsonb(vo),'operatorEpoch',vo.xmin::text,'operatorPrincipalEpoch',va.xmin::text)
  FROM places p JOIN venues v ON v.place_id=p.id AND v.city_id=p.city_id JOIN venue_candidates vc ON vc.id=v.source_candidate_id
  JOIN business_venue_relations vr ON vr.place_id=p.id AND vr.business_id=b.id
  JOIN business_console_venue_facts vf ON vf.place_id=p.id AND vf.business_id=b.id
  JOIN accounts vra ON vra.id=vf.reviewed_by JOIN business_review_grants vg ON vg.business_id=b.id AND vg.reviewer_account_id=vra.id
   JOIN accounts vsa ON vsa.id=vf.submitted_by JOIN business_memberships vsm ON vsm.business_id=b.id AND vsm.user_account_id=vsa.id
   LEFT JOIN organizations vo ON vo.id=v.operator_organization_id LEFT JOIN accounts va ON va.id=vo.account_id
  WHERE kind='PLACE' AND p.id=target AND p.publication_status='published' AND p.updated_at<=at AND (p.expires_at IS NULL OR p.expires_at>at)
   AND v.expires_at>at AND vc.status='approved' AND vc.place_id=p.id AND vc.city_id=p.city_id AND vc.reviewed_by=v.reviewed_by AND (v.operator_organization_id IS NULL OR (vo.status='active' AND va.status='active' AND va.account_type='organization')) AND vsa.status='active' AND vsa.account_type='person' AND vsm.status='active' AND vsm.role IN ('owner','admin') AND vr.status='verified' AND vf.state='verified' AND vf.valid_until>at AND vf.updated_at<=at
   AND vra.status='active' AND vra.account_type='person' AND vg.state='active' AND 'venue'=ANY(vg.permissions) AND vg.valid_from<=at AND vg.valid_until>at
   AND NOT EXISTS(SELECT 1 FROM business_memberships mm WHERE mm.business_id=b.id AND mm.user_account_id=vra.id AND mm.status='active')
 ) src ON true JOIN cities c ON c.id=src.city_id
 WHERE b.id=bid AND b.status='active' AND b.claim_status='verified' AND bp.account_type='business' AND bp.status='active'
 AND cl.state='verified' AND cl.reviewed_by=cr.id AND cl.updated_at<=at AND b.claim_reviewed_by=cr.id
 AND cp.account_type='person' AND cp.status='active' AND cm.status='active' AND cm.role IN ('owner','admin')
 AND cr.account_type='person' AND cr.status='active' AND cg.state='active' AND 'claim'=ANY(cg.permissions) AND cg.valid_from<=at AND cg.valid_until>at
 AND NOT EXISTS(SELECT 1 FROM business_memberships mm WHERE mm.business_id=b.id AND mm.user_account_id=cr.id AND mm.status='active')
 AND a.account_type='person' AND a.status='active' AND m.status='active' AND m.role IN ('owner','admin') AND m.updated_at<=at
 AND c.publication_status='published' AND c.updated_at<=at AND (c.expires_at IS NULL OR c.expires_at>at);
$$;

CREATE FUNCTION birdtie_sponsor_review_snapshot(city text,person uuid,at timestamptz) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('personEpoch',a.xmin::text,'editor',to_jsonb(m),'editorEpoch',m.xmin::text,'grant',to_jsonb(g),'grantEpoch',g.xmin::text,'cityEpoch',c.xmin::text)::text,'UTF8')),'hex')
 FROM accounts a JOIN city_editor_memberships m ON m.account_id=a.id AND m.city_id=city
 JOIN sponsored_opportunity_review_grants g ON g.reviewer_account_id=a.id AND g.city_id=city JOIN cities c ON c.id=city
 WHERE a.id=person AND a.account_type='person' AND a.status='active' AND m.state='active' AND m.role='reviewer' AND m.created_at<=at
 AND g.state='active' AND g.permission='review_sponsorship' AND g.valid_from<=at AND g.valid_until>at AND g.created_at<=at
 AND c.publication_status='published' AND c.updated_at<=at AND (c.expires_at IS NULL OR c.expires_at>at);
$$;
COMMIT;
