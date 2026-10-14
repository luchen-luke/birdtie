BEGIN;
ALTER TABLE organization_memberships ADD COLUMN control_version bigint NOT NULL DEFAULT 1 CHECK(control_version BETWEEN 1 AND 9007199254740991);
CREATE TABLE organization_human_authority (
 organization_id uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991)
);
INSERT INTO organization_human_authority(organization_id) SELECT id FROM organizations;
CREATE FUNCTION birdtie_organization_member_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.control_version>=9007199254740991 THEN RAISE EXCEPTION 'organization member version exhausted'; END IF;
  NEW.control_version:=OLD.control_version+1;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_member_control_version BEFORE UPDATE ON organization_memberships FOR EACH ROW EXECUTE FUNCTION birdtie_organization_member_version();
CREATE FUNCTION birdtie_organization_authority_version() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oid uuid;
BEGIN
 oid:=COALESCE(NEW.organization_id,OLD.organization_id);
 INSERT INTO organization_human_authority(organization_id,version) VALUES(oid,1)
 ON CONFLICT(organization_id) DO UPDATE SET version=organization_human_authority.version+1;
 RETURN COALESCE(NEW,OLD);
END $$;
CREATE TRIGGER organization_member_authority_version AFTER INSERT OR UPDATE OR DELETE ON organization_memberships FOR EACH ROW EXECUTE FUNCTION birdtie_organization_authority_version();
CREATE TABLE organization_map_proposals (
 id uuid PRIMARY KEY,
 organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 city_id text NOT NULL REFERENCES cities(id),
 latitude double precision NOT NULL CHECK(latitude BETWEEN -90 AND 90),
 longitude double precision NOT NULL CHECK(longitude BETWEEN -180 AND 180),
 canonical_revision bigint NOT NULL CHECK(canonical_revision BETWEEN 0 AND 9007199254740991),
 submitted_by uuid NOT NULL REFERENCES accounts(id),
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN('PENDING','APPROVED','REJECTED','WITHDRAWN')),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991),
 reviewed_by uuid REFERENCES accounts(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(status NOT IN('APPROVED','REJECTED') OR reviewed_by IS NOT NULL AND reviewed_by<>submitted_by)
);
CREATE INDEX organization_map_proposal_current ON organization_map_proposals(organization_id,created_at DESC,id);
CREATE FUNCTION birdtie_organization_proposal_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.status<>'PENDING' OR NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.city_id<>OLD.city_id OR NEW.latitude<>OLD.latitude OR NEW.longitude<>OLD.longitude OR NEW.canonical_revision<>OLD.canonical_revision OR NEW.submitted_by<>OLD.submitted_by OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'immutable organization proposal'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_proposal_immutable BEFORE UPDATE OR DELETE ON organization_map_proposals FOR EACH ROW EXECUTE FUNCTION birdtie_organization_proposal_immutable();
CREATE TABLE organization_human_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 organization_id uuid NOT NULL REFERENCES organizations(id),
 request_sha256 text NOT NULL CHECK(request_sha256 ~ '^[a-f0-9]{64}$'),
 action text NOT NULL CHECK(action IN('INVITE','ACCEPT','ROLE','REVOKE','PROPOSE','APPROVE_POINT','REJECT_POINT','WITHDRAW_POINT','HIDE_POINT','STOP')),
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 authority_version bigint NOT NULL CHECK(authority_version BETWEEN 0 AND 9007199254740991),
 member_version bigint NOT NULL DEFAULT 0 CHECK(member_version BETWEEN 0 AND 9007199254740991),
 canonical_revision bigint NOT NULL DEFAULT 0 CHECK(canonical_revision BETWEEN 0 AND 9007199254740991),
 proposal_version bigint NOT NULL DEFAULT 0 CHECK(proposal_version BETWEEN 0 AND 9007199254740991),
 membership_id uuid,proposal_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='STOPPED' AND action='STOP' AND authority_version=0 AND member_version=0 AND canonical_revision=0 AND proposal_version=0 AND membership_id IS NULL AND proposal_id IS NULL) OR (status='SUCCEEDED' AND action<>'STOP' AND authority_version>0))
);
CREATE FUNCTION birdtie_organization_operation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable organization operation'; END $$;
CREATE TRIGGER organization_operation_immutable BEFORE UPDATE OR DELETE ON organization_human_operations FOR EACH ROW EXECUTE FUNCTION birdtie_organization_operation_immutable();
-- A private seal, never a directory projection. Source rows include xmin so
-- restore-to-same-values cannot revive an older approval or read lease.
CREATE FUNCTION birdtie_organization_human_source(oid uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT md5(jsonb_build_array(
 (SELECT jsonb_build_array(to_jsonb(o),o.xmin::text) FROM organizations o WHERE id=oid),
 (SELECT to_jsonb(v) FROM organization_human_authority v WHERE organization_id=oid),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(m),m.xmin::text) ORDER BY m.id) FROM organization_memberships m WHERE organization_id=oid),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(a),a.xmin::text) ORDER BY a.id) FROM accounts a WHERE a.id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid) OR a.id IN(SELECT account_id FROM city_editor_memberships) OR a.id IN(SELECT account_id FROM organizations WHERE id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(p),p.xmin::text) ORDER BY p.account_id) FROM user_profiles p WHERE p.account_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(a),a.xmin::text) ORDER BY a.id) FROM agents a WHERE principal_account_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(p),p.xmin::text) ORDER BY p.agent_id) FROM agent_profiles p WHERE owner_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(m),m.xmin::text) ORDER BY m.city_id,m.account_id) FROM city_editor_memberships m),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(b),b.xmin::text) ORDER BY b.blocker_account_id,b.blocked_account_id) FROM account_blocks b WHERE b.blocker_account_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid) OR b.blocked_account_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(c),c.xmin::text) ORDER BY c.id) FROM cities c WHERE c.id IN(SELECT city_id FROM organization_map_proposals WHERE organization_id=oid)),
 (SELECT jsonb_build_array(to_jsonb(l),l.xmin::text) FROM organization_map_locations l WHERE organization_id=oid),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(p),p.xmin::text) ORDER BY p.id) FROM organization_map_proposals p WHERE organization_id=oid),
 (SELECT jsonb_agg(jsonb_build_array(id,authorization_generation) ORDER BY id) FROM sessions WHERE account_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(v),v.xmin::text) ORDER BY v.owner_id) FROM agent_profile_field_visibility v WHERE owner_id IN(SELECT user_account_id FROM organization_memberships WHERE organization_id=oid)),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(t),t.xmin::text) ORDER BY t.id) FROM person_ties t),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.id) FROM connection_requests r),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(m),m.xmin::text) ORDER BY m.id) FROM community_memberships m),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(c),c.xmin::text) ORDER BY c.id) FROM communities c)
 )::text)
$$;
COMMIT;
