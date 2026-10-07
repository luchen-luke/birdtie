BEGIN;
ALTER TABLE activity_participations
 ADD COLUMN disclosure_revision bigint NOT NULL DEFAULT 1,
 ADD COLUMN disclosure_visibility text NOT NULL DEFAULT 'private',
 ADD COLUMN disclosure_approved_at timestamptz,
 ADD COLUMN disclosure_expires_at timestamptz,
 ADD COLUMN disclosure_source_revision bigint,
 ADD COLUMN disclosure_activity_revision bigint,
 ADD COLUMN disclosure_source_digest text;
ALTER TABLE activity_participations ADD CONSTRAINT participation_disclosure_shape CHECK (
 disclosure_revision>0 AND (
 (disclosure_visibility='private' AND num_nonnulls(disclosure_approved_at,disclosure_expires_at,disclosure_source_revision,disclosure_activity_revision,disclosure_source_digest)=0) OR
 (disclosure_visibility='public' AND status='going' AND cancelled_at IS NULL
 AND num_nonnulls(disclosure_approved_at,disclosure_expires_at,disclosure_source_revision,disclosure_activity_revision,disclosure_source_digest)=5
 AND disclosure_expires_at>disclosure_approved_at AND disclosure_expires_at<=disclosure_approved_at+interval '24 hours'
 AND disclosure_source_revision=disclosure_revision AND disclosure_activity_revision>0 AND disclosure_source_digest ~ '^[0-9a-f]{64}$')));
CREATE FUNCTION birdtie_participation_disclosure_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.disclosure_revision<>1 OR NEW.disclosure_visibility<>'private' THEN RAISE EXCEPTION 'participation starts private at version one'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.disclosure_revision<>OLD.disclosure_revision THEN RAISE EXCEPTION 'participation disclosure revision is server managed'; END IF;
 IF ROW(NEW.id,NEW.activity_id,NEW.participant_account_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.activity_id,OLD.participant_account_id,OLD.created_at) THEN RAISE EXCEPTION 'participation stable identity cannot be remapped'; END IF;
 IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NEW; END IF;
 NEW.disclosure_revision:=OLD.disclosure_revision+1;
 -- No Activity lock here: Cancel already owns Participation. Preserve all old
 -- facts/IDs/updated_at; only remove disclosure when original facts change.
 IF ROW(NEW.id,NEW.activity_id,NEW.participant_account_id,NEW.status,NEW.cancelled_at,NEW.created_at,NEW.updated_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.activity_id,OLD.participant_account_id,OLD.status,OLD.cancelled_at,OLD.created_at,OLD.updated_at) THEN
  NEW.disclosure_visibility:='private';NEW.disclosure_approved_at:=NULL;NEW.disclosure_expires_at:=NULL;
  NEW.disclosure_source_revision:=NULL;NEW.disclosure_activity_revision:=NULL;NEW.disclosure_source_digest:=NULL;
 ELSIF NEW.disclosure_visibility='public' THEN NEW.disclosure_source_revision:=NEW.disclosure_revision;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER participation_disclosure_version BEFORE INSERT OR UPDATE ON activity_participations FOR EACH ROW EXECUTE FUNCTION birdtie_participation_disclosure_version();
CREATE FUNCTION birdtie_participation_disclosure_audit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='TRUNCATE' THEN
  IF EXISTS(SELECT 1 FROM audit_events WHERE resource_type='activity_participation_disclosure' OR purpose='HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE') THEN RAISE EXCEPTION 'participation disclosure history prevents truncate'; END IF;
  RETURN NULL;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') AND (OLD.resource_type='activity_participation_disclosure' OR OLD.purpose='HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE') THEN RAISE EXCEPTION 'participation disclosure audit append only'; END IF;
 IF TG_OP<>'DELETE' AND (NEW.resource_type='activity_participation_disclosure' OR NEW.purpose='HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE') THEN
  IF TG_OP='UPDATE' OR NEW.resource_type<>'activity_participation_disclosure' OR NEW.purpose<>'HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE' OR NEW.action NOT IN ('PUBLIC','PRIVATE') OR NEW.decision<>'allowed' OR NEW.actor_account_id IS NULL OR NOT EXISTS(SELECT 1 FROM activity_participations p WHERE p.id::text=NEW.resource_id AND p.participant_account_id=NEW.actor_account_id) THEN RAISE EXCEPTION 'invalid participation disclosure audit'; END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
CREATE TRIGGER participation_disclosure_audit_guard BEFORE INSERT OR UPDATE OR DELETE ON audit_events FOR EACH ROW EXECUTE FUNCTION birdtie_participation_disclosure_audit_guard();
CREATE TRIGGER participation_disclosure_audit_truncate_guard BEFORE TRUNCATE ON audit_events FOR EACH STATEMENT EXECUTE FUNCTION birdtie_participation_disclosure_audit_guard();
CREATE INDEX participation_disclosure_audit_epoch ON audit_events(actor_account_id,resource_id,id DESC) WHERE resource_type='activity_participation_disclosure' AND purpose='HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE';

-- One minimal current public Activity source, shared by approval and human
-- introductions. No raw private row/name/rights/contact projection.
CREATE FUNCTION birdtie_participation_public_source(target uuid,observer uuid,at_time timestamptz)
 RETURNS TABLE(available boolean,digest text,expires_at timestamptz) LANGUAGE sql STABLE AS $$
 SELECT
 coalesce(a.visibility='public' AND a.publication_status='published' AND a.cancelled_at IS NULL AND a.ends_at>at_time
 AND (a.expires_at IS NULL OR a.expires_at>at_time) AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>at_time)
 AND (a.place_id IS NULL OR (p.id IS NOT NULL AND p.city_id=a.city_id AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>at_time)))
 AND (a.venue_place_id IS NULL OR (a.venue_place_id=a.place_id AND p.id=v.place_id AND p.city_id=a.city_id AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>at_time) AND v.city_id=a.city_id AND v.expires_at>at_time AND vc.status='approved' AND vc.expires_at>at_time AND vc.place_id=v.place_id AND vc.city_id=v.city_id AND vc.reviewed_by=v.reviewed_by))
 AND host.status='active' AND (
 (ao.person_account_id IS NOT NULL AND host.id=ao.person_account_id AND host.account_type='person') OR
 (ao.community_id IS NOT NULL AND g.visibility='public' AND g.publication_status='published' AND g.lifecycle_status='active' AND g.owner_confirmed_at IS NOT NULL AND (g.expires_at IS NULL OR g.expires_at>at_time) AND (g.city_id IS NULL OR (gc.publication_status='published' AND (gc.expires_at IS NULL OR gc.expires_at>at_time)))) OR
 (ao.organization_id IS NOT NULL AND o.visibility='public' AND o.status='active' AND host.id=o.account_id AND host.account_type='organization') OR
 (ao.business_id IS NOT NULL AND b.status='active' AND b.claim_status='verified' AND host.id=b.account_id AND host.account_type='business'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=observer AND bl.blocked_account_id=a.host_account_id) OR (bl.blocker_account_id=a.host_account_id AND bl.blocked_account_id=observer)),false),
 encode(sha256(convert_to(jsonb_build_array(a.id,a.xmin::text,a.revision,a.city_id,a.place_id,a.venue_place_id,a.host_account_id,a.visibility,a.publication_status,a.cancelled_at,a.starts_at,a.ends_at,a.expires_at,
 city.id,city.xmin::text,p.id,p.xmin::text,v.place_id,v.xmin::text,vc.id,vc.xmin::text,ao.xmin::text,host.id,host.xmin::text,g.id,g.xmin::text,gc.id,gc.xmin::text,o.id,o.xmin::text,b.id,b.xmin::text)::text,'UTF8')),'hex'),
 least(a.ends_at,a.expires_at,city.expires_at,p.expires_at,v.expires_at,vc.expires_at,g.expires_at,gc.expires_at)
 FROM activities a LEFT JOIN cities city ON city.id=a.city_id LEFT JOIN activity_organizers ao ON ao.activity_id=a.id LEFT JOIN accounts host ON host.id=a.host_account_id
 LEFT JOIN places p ON p.id=a.place_id LEFT JOIN venues v ON v.place_id=a.venue_place_id LEFT JOIN venue_candidates vc ON vc.id=v.source_candidate_id
 LEFT JOIN communities g ON g.id=ao.community_id LEFT JOIN cities gc ON gc.id=g.city_id LEFT JOIN organizations o ON o.id=ao.organization_id LEFT JOIN businesses b ON b.id=ao.business_id
 WHERE a.id=target;
$$;
COMMIT;
