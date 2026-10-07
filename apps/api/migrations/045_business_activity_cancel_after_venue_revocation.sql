BEGIN;

-- Cancellation removes a published offer. A revoked Venue must not trap it
-- in the published state; all other Business writes still require the claim.
CREATE OR REPLACE FUNCTION birdtie_validate_activity_organization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE organization_account uuid; creator_type text; business_id uuid;
BEGIN
    IF NEW.organization_id IS NOT NULL THEN
        SELECT account_id INTO organization_account FROM organizations WHERE id=NEW.organization_id;
        IF organization_account IS NULL OR NEW.host_account_id IS DISTINCT FROM organization_account THEN
            RAISE EXCEPTION 'organization activity requires its organization principal as host';
        END IF;
    END IF;
    IF NEW.created_by_account_id IS NOT NULL THEN
        SELECT account_type INTO creator_type FROM accounts WHERE id=NEW.created_by_account_id;
        IF creator_type IS DISTINCT FROM 'person' THEN
            RAISE EXCEPTION 'activity creator must be a person account';
        END IF;
    END IF;
    SELECT b.id INTO business_id FROM businesses b WHERE b.account_id=NEW.host_account_id;
    IF business_id IS NOT NULL AND TG_OP='UPDATE' AND OLD.cancelled_at IS NULL
       AND NEW.cancelled_at IS NOT NULL AND
       (to_jsonb(NEW) - 'cancelled_at' - 'revision' - 'updated_at') =
       (to_jsonb(OLD) - 'cancelled_at' - 'revision' - 'updated_at') THEN
        RETURN NEW;
    END IF;
    IF business_id IS NOT NULL AND (NEW.organization_id IS NOT NULL OR NOT EXISTS(
        SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
        WHERE b.id=business_id AND b.status='active' AND b.claim_status='verified'
          AND a.status='active' AND (NEW.place_id IS NULL OR
              birdtie_verified_business_venue(b.id,NEW.place_id)))) THEN
        RAISE EXCEPTION 'business Activity requires verified principal and venue relation';
    END IF;
    RETURN NEW;
END $$;

COMMIT;
