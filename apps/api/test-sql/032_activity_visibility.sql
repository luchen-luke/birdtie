-- Existing Organization Activity visibility matrix; every change rolls back.
BEGIN;
DO $$
DECLARE v_activity uuid;v_member uuid;v_outsider uuid;
BEGIN
    SELECT a.id,m.user_account_id INTO v_activity,v_member
    FROM activities a JOIN organization_memberships m ON m.organization_id=a.organization_id
    WHERE a.publication_status='published' AND m.status='active' ORDER BY a.id LIMIT 1;
    IF v_activity IS NULL THEN RAISE EXCEPTION 'test requires a published Organization Activity'; END IF;
    SELECT id INTO v_outsider FROM accounts WHERE account_type='person' AND status='active'
      AND id NOT IN (SELECT user_account_id FROM organization_memberships
                     WHERE organization_id=(SELECT organization_id FROM activities WHERE id=v_activity)
                       AND status='active') ORDER BY id LIMIT 1;
    IF v_outsider IS NULL THEN RAISE EXCEPTION 'test requires an outsider'; END IF;
    UPDATE activities SET visibility='organizer_members' WHERE id=v_activity;
    IF NOT birdtie_activity_visible_to(v_activity,v_member) OR
       birdtie_activity_visible_to(v_activity,v_outsider) OR
       birdtie_activity_visible_to(v_activity,NULL) THEN
        RAISE EXCEPTION 'Organization members-only ACL incorrect';
    END IF;
    UPDATE activities SET visibility='invite_only' WHERE id=v_activity;
    IF birdtie_activity_visible_to(v_activity,v_outsider) THEN
        RAISE EXCEPTION 'uninvited outsider can see invite-only Activity';
    END IF;
    INSERT INTO activity_invitations(activity_id,invitee_account_id,invited_by_account_id)
    VALUES(v_activity,v_outsider,v_member);
    IF NOT birdtie_activity_visible_to(v_activity,v_outsider) THEN
        RAISE EXCEPTION 'explicit invite did not grant access';
    END IF;
END $$;
ROLLBACK;
