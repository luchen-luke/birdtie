BEGIN;

-- A Follow is one Person's one-way interest in a public target. It does not
-- create a Tie, membership, consent, or Agent capability.
CREATE TABLE follows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    follower_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    person_account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    community_id uuid REFERENCES communities(id) ON DELETE CASCADE,
    business_id uuid REFERENCES businesses(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT follows_one_target CHECK
        (num_nonnulls(person_account_id,organization_id,community_id,business_id)=1),
    CONSTRAINT follows_not_self CHECK
        (person_account_id IS NULL OR follower_account_id<>person_account_id)
);
CREATE UNIQUE INDEX follows_one_person ON follows(follower_account_id,person_account_id)
    WHERE person_account_id IS NOT NULL;
CREATE UNIQUE INDEX follows_one_organization ON follows(follower_account_id,organization_id)
    WHERE organization_id IS NOT NULL;
CREATE UNIQUE INDEX follows_one_community ON follows(follower_account_id,community_id)
    WHERE community_id IS NOT NULL;
CREATE UNIQUE INDEX follows_one_business ON follows(follower_account_id,business_id)
    WHERE business_id IS NOT NULL;
CREATE INDEX follows_follower_recent ON follows(follower_account_id,created_at DESC,id DESC);

CREATE OR REPLACE FUNCTION birdtie_validate_follow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.follower_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Follow requires active Person';
    END IF;
    IF NEW.person_account_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM accounts a JOIN user_profiles p ON p.account_id=a.id
            WHERE a.id=NEW.person_account_id AND a.account_type='person'
            AND a.status='active' AND p.visibility='public') OR
           EXISTS (SELECT 1 FROM account_blocks b WHERE
            (b.blocker_account_id=NEW.follower_account_id AND b.blocked_account_id=NEW.person_account_id) OR
            (b.blocker_account_id=NEW.person_account_id AND b.blocked_account_id=NEW.follower_account_id)) THEN
            RAISE EXCEPTION 'Person Follow target unavailable';
        END IF;
    ELSIF NEW.organization_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM organizations o JOIN accounts a ON a.id=o.account_id
            WHERE o.id=NEW.organization_id AND o.status='active' AND a.status='active') THEN
            RAISE EXCEPTION 'Organization Follow target unavailable';
        END IF;
    ELSIF NEW.community_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM communities c WHERE c.id=NEW.community_id
            AND c.lifecycle_status='active' AND c.publication_status='published'
            AND c.visibility='public') THEN
            RAISE EXCEPTION 'Community Follow target unavailable';
        END IF;
    ELSIF NEW.business_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
            WHERE b.id=NEW.business_id AND b.status='active' AND b.claim_status='verified'
            AND a.status='active') THEN
            RAISE EXCEPTION 'Business Follow target unavailable';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER follows_validate BEFORE INSERT OR UPDATE OF
    follower_account_id,person_account_id,organization_id,community_id,business_id
    ON follows FOR EACH ROW EXECUTE FUNCTION birdtie_validate_follow();

-- Blocking either direction removes existing one-way Follows atomically,
-- including rows inserted outside the HTTP path.
CREATE OR REPLACE FUNCTION birdtie_block_removes_follows() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM follows WHERE
        (follower_account_id=NEW.blocker_account_id AND person_account_id=NEW.blocked_account_id) OR
        (follower_account_id=NEW.blocked_account_id AND person_account_id=NEW.blocker_account_id);
    RETURN NEW;
END $$;
CREATE TRIGGER account_block_removes_follows AFTER INSERT ON account_blocks
    FOR EACH ROW EXECUTE FUNCTION birdtie_block_removes_follows();

COMMIT;
