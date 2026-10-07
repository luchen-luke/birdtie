-- Run against a local migrated database. All synthetic records roll back.
BEGIN;
DO $$
DECLARE creator uuid; other_person uuid; v_community_id uuid;
BEGIN
    SELECT id INTO creator FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1;
    SELECT id INTO other_person FROM accounts WHERE account_type='person' AND id<>creator ORDER BY id LIMIT 1;
    IF creator IS NULL OR other_person IS NULL THEN
        RAISE EXCEPTION 'test requires two existing local person accounts';
    END IF;
    INSERT INTO communities (city_id,owner_account_id,name,summary,visibility,
        publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label,
        join_policy,lifecycle_status)
    VALUES (NULL,creator,'Synthetic Community','rollback-only','private','published',now(),
        'Birdtie test','synthetic:community','Test owner','request','active')
    RETURNING id INTO v_community_id;
    IF NOT EXISTS (SELECT 1 FROM community_memberships WHERE community_id=v_community_id
        AND user_account_id=creator AND role='owner' AND status='active') THEN
        RAISE EXCEPTION 'community owner was not inserted atomically';
    END IF;
    INSERT INTO community_memberships (community_id,user_account_id,role,status)
    VALUES (v_community_id,other_person,'member','pending');
    IF (SELECT count(*) FROM community_memberships m WHERE m.community_id=v_community_id) <> 2 THEN
        RAISE EXCEPTION 'membership insert failed';
    END IF;
END $$;
ROLLBACK;
