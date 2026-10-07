package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/jackc/pgx/v5"
)

const socialColumns = `c.id,c.name,c.summary,c.avatar_url,c.city_id,c.visibility,
    c.join_policy,c.lifecycle_status,c.owner_account_id,
    (SELECT count(*) FROM community_memberships cm WHERE cm.community_id=c.id AND cm.status='active'),
    (SELECT role FROM community_memberships cm WHERE cm.community_id=c.id AND cm.user_account_id=$1 AND cm.status='active'),
    (SELECT status FROM community_memberships cm WHERE cm.community_id=c.id AND cm.user_account_id=$1),
    c.created_at,c.updated_at`

func scanSocial(row scanner) (community.SocialRecord, error) {
	var out community.SocialRecord
	err := row.Scan(&out.ID, &out.Name, &out.Description, &out.AvatarURL, &out.CityID,
		&out.Visibility, &out.JoinPolicy, &out.Status, &out.CreatedBy, &out.MemberCount,
		&out.MyRole, &out.MyStatus, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func scanSocialMember(row scanner) (community.Membership, error) {
	var m community.Membership
	err := row.Scan(&m.ID, &m.CommunityID, &m.UserAccountID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func socialRole(ctx context.Context, tx pgx.Tx, actor, communityID string) (string, string, error) {
	var role, status string
	err := tx.QueryRow(ctx, `SELECT role,status FROM community_memberships
        WHERE community_id=$1 AND user_account_id=$2 FOR SHARE`, communityID, actor).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return role, status, err
}

func socialAudit(ctx context.Context, tx pgx.Tx, actor, action, id string) error {
	_, err := auditExec(ctx, tx, `INSERT INTO audit_events
        (actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES ($1,$2,'community',$3,'allowed','community_social_layer')`, actor, action, id)
	return err
}

func (s *Store) CreateSocialCommunity(ctx context.Context, actor string, input community.SocialInput) (community.SocialRecord, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return community.SocialRecord{}, err
	}
	defer tx.Rollback(context.Background())
	out, err := createSocialCommunityInTx(ctx, tx, actor, input)
	if err != nil {
		return community.SocialRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return community.SocialRecord{}, err
	}
	return out, nil
}

func createSocialCommunityInTx(ctx context.Context, tx pgx.Tx, actor string, input community.SocialInput) (community.SocialRecord, error) {
	var err error
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO communities
        (city_id,owner_account_id,name,summary,avatar_url,visibility,join_policy,
         lifecycle_status,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label)
        SELECT NULLIF($2,''),a.id,$3,$4,NULLIF($5,''),$6,$7,'active','published',now(),
               'Birdtie Community','birdtie:community:' || gen_random_uuid()::text,
               '社区维护者'
        FROM accounts a WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
          AND ($2='' OR EXISTS(SELECT 1 FROM cities WHERE id=$2 AND publication_status='published'))
        RETURNING id`, actor, input.CityID, input.Name, input.Summary, input.AvatarURL,
		input.Visibility, input.JoinPolicy).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return community.SocialRecord{}, community.ErrForbidden
	}
	if err != nil {
		return community.SocialRecord{}, err
	}
	if err = socialAudit(ctx, tx, actor, "community_create", id); err != nil {
		return community.SocialRecord{}, err
	}
	return getSocialCommunityInTx(ctx, tx, actor, id)
}

func (s *Store) GetSocialCommunity(ctx context.Context, actor, id string) (community.SocialRecord, error) {
	return getSocialCommunityInTx(ctx, s.pool, actor, id)
}

func getSocialCommunityInTx(ctx context.Context, q socialReader, actor, id string) (community.SocialRecord, error) {
	out, err := scanSocial(q.QueryRow(ctx, `SELECT `+socialColumns+` FROM communities c
        WHERE c.id=$2 AND c.lifecycle_status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())`, actor, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return community.SocialRecord{}, community.ErrNotFound
	}
	if err != nil {
		return community.SocialRecord{}, err
	}
	// A hidden Community is only addressable by an active member or invitee.
	// Returning its name or membership count to a known ID would reveal it.
	if out.Visibility == "hidden" && out.MyRole == nil &&
		(out.MyStatus == nil || *out.MyStatus != "invited") {
		return community.SocialRecord{}, community.ErrNotFound
	}
	if out.Visibility != "public" && out.MyRole == nil {
		out.Description = ""
	}
	return out, nil
}

func (s *Store) ListSocialCommunities(ctx context.Context, actor, cityID string, mine bool) ([]community.SocialRecord, error) {
	return listSocialCommunitiesInTx(ctx, s.pool, actor, cityID, mine)
}

func listSocialCommunitiesInTx(ctx context.Context, q socialReader, actor, cityID string, mine bool) ([]community.SocialRecord, error) {
	rows, err := q.Query(ctx, `SELECT `+socialColumns+` FROM communities c
        WHERE c.lifecycle_status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
          AND ($2='' OR c.city_id=$2)
          AND (CASE WHEN $3 THEN EXISTS(SELECT 1 FROM community_memberships m
                   WHERE m.community_id=c.id AND m.user_account_id=$1
                     AND (m.status IN ('active','invited') OR
                          (m.status='pending' AND c.visibility<>'hidden')))
               ELSE c.visibility<>'hidden' END)
        ORDER BY c.created_at DESC,c.id DESC LIMIT 100`, actor, cityID, mine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]community.SocialRecord, 0)
	for rows.Next() {
		item, e := scanSocial(rows)
		if e != nil {
			return nil, e
		}
		if item.Visibility != "public" && item.MyRole == nil {
			item.Description = ""
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSocialCommunity(ctx context.Context, actor, id string, input community.SocialInput) (community.SocialRecord, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return community.SocialRecord{}, err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return community.SocialRecord{}, err
	}
	out, err := updateSocialCommunityInTx(ctx, tx, actor, id, input)
	if err != nil {
		return community.SocialRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return community.SocialRecord{}, err
	}
	return out, nil
}

func updateSocialCommunityInTx(ctx context.Context, tx pgx.Tx, actor, id string, input community.SocialInput) (community.SocialRecord, error) {
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return community.SocialRecord{}, err
	}
	if status != "active" || (role != "owner" && role != "admin") {
		return community.SocialRecord{}, community.ErrForbidden
	}
	var updated string
	err = tx.QueryRow(ctx, `UPDATE communities SET name=$2,summary=$3,avatar_url=NULLIF($4,''),
          visibility=$5,join_policy=$6,city_id=NULLIF($7,''),updated_at=now()
        WHERE id=$1 AND lifecycle_status='active' AND publication_status='published'
          AND ($7='' OR EXISTS(SELECT 1 FROM cities WHERE id=$7 AND publication_status='published'))
        RETURNING id`, id, input.Name, input.Summary, input.AvatarURL, input.Visibility, input.JoinPolicy, input.CityID).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return community.SocialRecord{}, community.ErrNotFound
	}
	if err != nil {
		return community.SocialRecord{}, err
	}
	if err = socialAudit(ctx, tx, actor, "community_update", id); err != nil {
		return community.SocialRecord{}, err
	}
	return getSocialCommunityInTx(ctx, tx, actor, id)
}

func (s *Store) ArchiveSocialCommunity(ctx context.Context, actor, id string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return err
	}
	if err = archiveSocialCommunityInTx(ctx, tx, actor, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func archiveSocialCommunityInTx(ctx context.Context, tx pgx.Tx, actor, id string) error {
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if status != "active" || role != "owner" {
		return community.ErrForbidden
	}
	tag, err := tx.Exec(ctx, `UPDATE communities SET lifecycle_status='archived',updated_at=now()
        WHERE id=$1 AND lifecycle_status='active'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return community.ErrNotFound
	}
	if err = socialAudit(ctx, tx, actor, "community_archive", id); err != nil {
		return err
	}
	return nil
}

func (s *Store) JoinSocialCommunity(ctx context.Context, actor, id string) (community.Membership, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return community.Membership{}, err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return community.Membership{}, err
	}
	out, err := joinSocialCommunityInTx(ctx, tx, actor, id)
	if err != nil {
		return community.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func joinSocialCommunityInTx(ctx context.Context, tx pgx.Tx, actor, id string) (community.Membership, error) {
	var err error
	var policy, visibility string
	err = tx.QueryRow(ctx, `SELECT join_policy,visibility FROM communities WHERE id=$1 AND lifecycle_status='active'
        AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR UPDATE`, id).Scan(&policy, &visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		return community.Membership{}, community.ErrNotFound
	}
	if err != nil {
		return community.Membership{}, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2 FOR UPDATE`, id, actor).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return community.Membership{}, err
	}
	if visibility == "hidden" && existing != "invited" && existing != "active" {
		return community.Membership{}, community.ErrNotFound
	}
	if existing == "active" || existing == "pending" {
		return community.Membership{}, community.ErrConflict
	}
	if policy == "invite_only" && existing != "invited" {
		return community.Membership{}, community.ErrForbidden
	}
	state := "pending"
	if policy == "open" || existing == "invited" {
		state = "active"
	}
	out, err := scanSocialMember(tx.QueryRow(ctx, `INSERT INTO community_memberships
        (community_id,user_account_id,role,status) VALUES($1,$2,'member',$3)
        ON CONFLICT(community_id,user_account_id) DO UPDATE SET role='member',status=$3,updated_at=now()
        RETURNING id,community_id,user_account_id,role,status,created_at,updated_at`, id, actor, state))
	if err != nil {
		return community.Membership{}, err
	}
	if err = socialAudit(ctx, tx, actor, "community_join", id); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func (s *Store) LeaveSocialCommunity(ctx context.Context, actor, id string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return err
	}
	if err = leaveSocialCommunityInTx(ctx, tx, actor, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func leaveSocialCommunityInTx(ctx context.Context, tx pgx.Tx, actor, id string) error {
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if status == "" || status == "left" {
		return community.ErrNotFound
	}
	if role == "owner" {
		return community.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE community_memberships SET status='left',updated_at=now()
        WHERE community_id=$1 AND user_account_id=$2`, id, actor)
	if err != nil {
		return err
	}
	if err = socialAudit(ctx, tx, actor, "community_leave", id); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListSocialMembers(ctx context.Context, actor, id string, requests bool) ([]community.Membership, error) {
	return listSocialMembersInTx(ctx, s.pool, actor, id, requests)
}

func listSocialMembersInTx(ctx context.Context, q socialReader, actor, id string, requests bool, includeInvites ...bool) ([]community.Membership, error) {
	var visibility string
	err := q.QueryRow(ctx, `SELECT visibility FROM communities WHERE id=$1 AND lifecycle_status='active'`, id).Scan(&visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, community.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var role, status string
	err = q.QueryRow(ctx, `SELECT role,status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, id, actor).Scan(&role, &status)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if requests && !(status == "active" && (role == "owner" || role == "admin")) {
		return nil, community.ErrForbidden
	}
	if !requests && status != "active" {
		return nil, community.ErrNotFound
	}
	wanted := "active"
	if requests {
		wanted = "pending"
	}
	rows, err := q.Query(ctx, `SELECT m.id,m.community_id,m.user_account_id,m.role,m.status,m.created_at,m.updated_at,
        CASE WHEN birdtie_agent_profile_field_allowed(a.id,$3::uuid,'displayName')
            THEN COALESCE(NULLIF(p.display_name,''),NULLIF(a.handle,''),'Birdtie 成员')
            ELSE 'Birdtie 成员' END
        FROM community_memberships m JOIN accounts a ON a.id=m.user_account_id
        LEFT JOIN user_profiles p ON p.account_id=a.id
        WHERE m.community_id=$1 AND (m.status=$2 OR ($4::boolean AND m.status='invited'))
        AND EXISTS(SELECT 1 FROM communities current_group
            JOIN community_memberships current_viewer ON current_viewer.community_id=current_group.id
                AND current_viewer.user_account_id=$3 AND current_viewer.status='active'
            JOIN accounts viewer ON viewer.id=current_viewer.user_account_id
                AND viewer.account_type='person' AND viewer.status='active'
            WHERE current_group.id=m.community_id AND current_group.lifecycle_status='active'
                AND ($2='active' OR current_viewer.role IN ('owner','admin')))
        ORDER BY m.created_at,m.id LIMIT 500`, id, wanted, actor, requests && len(includeInvites) > 0 && includeInvites[0])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]community.Membership, 0)
	for rows.Next() {
		var m community.Membership
		e := rows.Scan(&m.ID, &m.CommunityID, &m.UserAccountID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt, &m.DisplayName)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DecideSocialRequest(ctx context.Context, actor, id, memberID string, approve bool) (community.Membership, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return community.Membership{}, err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return community.Membership{}, err
	}
	out, err := decideSocialRequestInTx(ctx, tx, actor, id, memberID, approve)
	if err != nil {
		return community.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func decideSocialRequestInTx(ctx context.Context, tx pgx.Tx, actor, id, memberID string, approve bool) (community.Membership, error) {
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return community.Membership{}, err
	}
	if status != "active" || (role != "owner" && role != "admin") {
		return community.Membership{}, community.ErrForbidden
	}
	state := "rejected"
	if approve {
		state = "active"
	}
	out, err := scanSocialMember(tx.QueryRow(ctx, `UPDATE community_memberships SET status=$3,updated_at=now()
        WHERE id=$1 AND community_id=$2 AND status='pending'
        RETURNING id,community_id,user_account_id,role,status,created_at,updated_at`, memberID, id, state))
	if errors.Is(err, pgx.ErrNoRows) {
		return community.Membership{}, community.ErrConflict
	}
	if err != nil {
		return community.Membership{}, err
	}
	action := "community_request_reject"
	if approve {
		action = "community_request_approve"
	}
	if err = socialAudit(ctx, tx, actor, action, id); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func (s *Store) InviteSocialMember(ctx context.Context, actor, id, target string) (community.Membership, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return community.Membership{}, err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return community.Membership{}, err
	}
	out, err := inviteSocialMemberInTx(ctx, tx, actor, id, target)
	if err != nil {
		return community.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func inviteSocialMemberInTx(ctx context.Context, tx pgx.Tx, actor, id, target string) (community.Membership, error) {
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return community.Membership{}, err
	}
	if status != "active" || (role != "owner" && role != "admin") {
		return community.Membership{}, community.ErrForbidden
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND account_type='person' AND status='active')`, target).Scan(&exists)
	if err != nil {
		return community.Membership{}, err
	}
	if !exists {
		return community.Membership{}, community.ErrNotFound
	}
	var old string
	err = tx.QueryRow(ctx, `SELECT status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2 FOR UPDATE`, id, target).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return community.Membership{}, err
	}
	if old == "active" || old == "pending" || old == "invited" {
		return community.Membership{}, community.ErrConflict
	}
	out, err := scanSocialMember(tx.QueryRow(ctx, `INSERT INTO community_memberships
        (community_id,user_account_id,role,status,invited_by_account_id)
        VALUES($1,$2,'member','invited',$3)
        ON CONFLICT(community_id,user_account_id) DO UPDATE SET role='member',status='invited',
          invited_by_account_id=$3,updated_at=now()
        RETURNING id,community_id,user_account_id,role,status,created_at,updated_at`, id, target, actor))
	if err != nil {
		return community.Membership{}, err
	}
	if err = socialAudit(ctx, tx, actor, "community_invite", id); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func (s *Store) ChangeSocialMemberRole(ctx context.Context, actor, id, memberID, newRole string) (community.Membership, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return community.Membership{}, err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return community.Membership{}, err
	}
	out, err := changeSocialMemberRoleInTx(ctx, tx, actor, id, memberID, newRole)
	if err != nil {
		return community.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func changeSocialMemberRoleInTx(ctx context.Context, tx pgx.Tx, actor, id, memberID, newRole string) (community.Membership, error) {
	if newRole != "admin" && newRole != "member" {
		return community.Membership{}, community.ErrValidation
	}
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return community.Membership{}, err
	}
	if role != "owner" || status != "active" {
		return community.Membership{}, community.ErrForbidden
	}
	out, err := scanSocialMember(tx.QueryRow(ctx, `UPDATE community_memberships SET role=$3,updated_at=now()
        WHERE id=$1 AND community_id=$2 AND status='active' AND role IN ('admin','member')
        RETURNING id,community_id,user_account_id,role,status,created_at,updated_at`, memberID, id, newRole))
	if errors.Is(err, pgx.ErrNoRows) {
		return community.Membership{}, community.ErrConflict
	}
	if err != nil {
		return community.Membership{}, err
	}
	if err = socialAudit(ctx, tx, actor, "community_member_role", id); err != nil {
		return community.Membership{}, err
	}
	return out, nil
}

func (s *Store) RemoveSocialMember(ctx context.Context, actor, id, memberID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return err
	}
	if err = removeSocialMemberInTx(ctx, tx, actor, id, memberID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func removeSocialMemberInTx(ctx context.Context, tx pgx.Tx, actor, id, memberID string) error {
	var err error
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if status != "active" || (role != "owner" && role != "admin") {
		return community.ErrForbidden
	}
	var targetRole, targetStatus string
	err = tx.QueryRow(ctx, `SELECT role,status FROM community_memberships WHERE id=$1 AND community_id=$2 FOR UPDATE`, memberID, id).Scan(&targetRole, &targetStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return community.ErrNotFound
	}
	if err != nil {
		return err
	}
	if targetRole == "owner" || (role == "admin" && targetRole != "member") {
		return community.ErrForbidden
	}
	if targetStatus != "active" && targetStatus != "pending" && targetStatus != "invited" {
		return community.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE community_memberships SET status='left',updated_at=now() WHERE id=$1`, memberID)
	if err != nil {
		return err
	}
	if err = socialAudit(ctx, tx, actor, "community_member_remove", id); err != nil {
		return err
	}
	return nil
}

func (s *Store) TransferSocialOwner(ctx context.Context, actor, id, target string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = socialLockCommunity(ctx, tx, id); err != nil {
		return err
	}
	if err = transferSocialOwnerInTx(ctx, tx, actor, id, target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func transferSocialOwnerInTx(ctx context.Context, tx pgx.Tx, actor, id, target string) error {
	var err error
	_, err = tx.Exec(ctx, `SELECT id FROM communities WHERE id=$1 AND lifecycle_status='active' FOR UPDATE`, id)
	if err != nil {
		return err
	}
	role, status, err := socialRole(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if role != "owner" || status != "active" {
		return community.ErrForbidden
	}
	if strings.EqualFold(actor, target) {
		return community.ErrConflict
	}
	newRole, newStatus, err := socialRole(ctx, tx, target, id)
	if err != nil {
		return err
	}
	if newStatus != "active" || newRole == "" {
		return community.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE community_memberships SET role='owner',updated_at=now()
        WHERE community_id=$1 AND user_account_id=$2`, id, target)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE community_memberships SET role='admin',updated_at=now()
        WHERE community_id=$1 AND user_account_id=$2`, id, actor)
	if err != nil {
		return err
	}
	if err = socialAudit(ctx, tx, actor, "community_owner_transfer", id); err != nil {
		return err
	}
	return nil
}

type socialReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func socialLockCommunity(ctx context.Context, tx pgx.Tx, id string) error {
	var locked string
	err := tx.QueryRow(ctx, `SELECT id FROM communities WHERE id=$1 AND lifecycle_status='active' AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return community.ErrNotFound
	}
	return err
}
