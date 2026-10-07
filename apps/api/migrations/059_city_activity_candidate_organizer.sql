BEGIN;

-- Never infer the host from a city editor, a reviewer, or an external label.
-- Historical candidate rows remain byte-for-byte unchanged and unbound.
CREATE TABLE city_activity_candidate_organizers (
    candidate_id uuid PRIMARY KEY REFERENCES activity_candidates(id) ON DELETE CASCADE,
    selected_by uuid NOT NULL REFERENCES accounts(id),
    person_account_id uuid REFERENCES accounts(id),
    community_id uuid REFERENCES communities(id),
    organization_id uuid REFERENCES organizations(id),
    business_id uuid REFERENCES businesses(id),
    selected_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT city_candidate_organizer_exactly_one CHECK
        (num_nonnulls(person_account_id,community_id,organization_id,business_id)=1),
    CONSTRAINT city_candidate_organizer_finite_time CHECK (isfinite(selected_at))
);

CREATE FUNCTION birdtie_guard_city_candidate_organizer() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE candidate_owner uuid; candidate_status text;
BEGIN
    IF TG_OP='DELETE' THEN
        IF EXISTS(SELECT 1 FROM activity_candidates WHERE id=OLD.candidate_id) THEN
            RAISE EXCEPTION 'candidate organizer selector may only be removed with its candidate';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' THEN
        RAISE EXCEPTION 'candidate organizer selector is immutable; submit a new candidate';
    END IF;
    SELECT submitted_by,status INTO candidate_owner,candidate_status
    FROM activity_candidates WHERE id=NEW.candidate_id;
    IF candidate_owner IS DISTINCT FROM NEW.selected_by OR candidate_status IS DISTINCT FROM 'pending'
       OR NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.selected_by AND account_type='person' AND status='active')
       OR NEW.selected_at>clock_timestamp() THEN
        RAISE EXCEPTION 'candidate organizer requires the actual active Person submitter';
    END IF;
    IF NEW.person_account_id IS NOT NULL AND NEW.person_account_id IS DISTINCT FROM NEW.selected_by THEN
        RAISE EXCEPTION 'Person organizer requires explicit self selection';
    END IF;
    IF NEW.community_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM communities c JOIN accounts owner ON owner.id=c.owner_account_id
        JOIN community_memberships m ON m.community_id=c.id
        WHERE c.id=NEW.community_id AND c.lifecycle_status='active' AND c.publication_status='published'
          AND c.visibility='public' AND owner.status='active'
          AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
          AND m.user_account_id=NEW.selected_by AND m.status='active' AND m.role IN ('owner','admin')) THEN
        RAISE EXCEPTION 'public Community organizer requires current manager';
    END IF;
    IF NEW.organization_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM organizations o JOIN accounts principal ON principal.id=o.account_id
        JOIN organization_memberships m ON m.organization_id=o.id
        WHERE o.id=NEW.organization_id AND o.status='active' AND o.visibility='public'
          AND principal.account_type='organization' AND principal.status='active'
          AND m.user_account_id=NEW.selected_by AND m.status='active' AND m.role IN ('owner','admin')) THEN
        RAISE EXCEPTION 'public Organization organizer requires current manager';
    END IF;
    IF NEW.business_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM businesses b JOIN accounts principal ON principal.id=b.account_id
        JOIN business_memberships m ON m.business_id=b.id
        WHERE b.id=NEW.business_id AND b.status='active' AND b.claim_status='verified'
          AND principal.account_type='business' AND principal.status='active'
          AND m.user_account_id=NEW.selected_by AND m.status='active' AND m.role IN ('owner','admin')) THEN
        RAISE EXCEPTION 'Business organizer requires current verified principal and manager';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER city_candidate_organizer_guard BEFORE INSERT OR UPDATE OR DELETE
    ON city_activity_candidate_organizers FOR EACH ROW EXECUTE FUNCTION birdtie_guard_city_candidate_organizer();

COMMIT;
