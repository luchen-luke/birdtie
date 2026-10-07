package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
)

const membershipColumns = `m.id,m.organization_id,m.user_account_id,
    COALESCE(CASE WHEN birdtie_agent_profile_field_allowed(m.user_account_id,$2::uuid,'displayName')
        THEN p.display_name END,'Birdtie 用户'),m.role,m.status`

func scanMembership(row pgx.Row) (organization.Membership, error) {
	var m organization.Membership
	err := row.Scan(&m.ID, &m.OrganizationID, &m.UserAccountID,
		&m.DisplayName, &m.Role, &m.Status)
	return m, err
}

func readMembership(ctx context.Context, tx pgx.Tx, id, actorID string) (organization.Membership, error) {
	return scanMembership(tx.QueryRow(ctx, `SELECT `+membershipColumns+`
        FROM organization_memberships m
        LEFT JOIN user_profiles p ON p.account_id=m.user_account_id
        WHERE m.id=$1`, id, actorID))
}

func (s *Store) ListMembers(ctx context.Context, actorID, orgID string) ([]organization.Membership, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+membershipColumns+`
        FROM organization_memberships m
        LEFT JOIN user_profiles p ON p.account_id=m.user_account_id
        WHERE m.organization_id=$1 AND m.status IN ('active','invited')
          AND EXISTS (SELECT 1 FROM organization_memberships actor
            JOIN organizations o ON o.id=actor.organization_id
            WHERE actor.organization_id=$1 AND actor.user_account_id=$2
              AND actor.status='active' AND actor.role IN ('owner','admin')
              AND o.status='active')
        ORDER BY m.created_at,m.id`, orgID, actorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []organization.Membership{}
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// No rows can mean either an empty roster or forbidden access. Every
	// organization has an owner, so an empty authorized roster is impossible.
	if len(out) == 0 {
		return nil, organization.ErrForbidden
	}
	return out, nil
}

func (s *Store) ListInvitations(ctx context.Context, actorID string) ([]organization.Membership, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+membershipColumns+`,o.name
        FROM organization_memberships m
        JOIN organizations o ON o.id=m.organization_id AND o.status='active'
        LEFT JOIN user_profiles p ON p.account_id=m.user_account_id
        WHERE m.user_account_id=$1 AND m.status='invited'
        ORDER BY m.created_at,m.id`, actorID, actorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []organization.Membership{}
	for rows.Next() {
		var m organization.Membership
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.UserAccountID, &m.DisplayName, &m.Role, &m.Status, &m.OrganizationName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) membershipTx(ctx context.Context, actorID, orgID string) (pgx.Tx, string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, "", err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT status='active' FROM organizations WHERE id=$1 FOR UPDATE`, orgID).Scan(&active); err != nil {
		tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", organization.ErrForbidden
		}
		return nil, "", err
	}
	if !active {
		tx.Rollback(ctx)
		return nil, "", organization.ErrForbidden
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT m.role FROM organization_memberships m
        JOIN accounts a ON a.id=m.user_account_id AND a.status='active' AND a.account_type='person'
        WHERE m.organization_id=$1 AND m.user_account_id=$2 AND m.status='active'`, orgID, actorID).Scan(&role)
	if err != nil {
		tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", organization.ErrForbidden
		}
		return nil, "", err
	}
	return tx, role, nil
}

func membershipAudit(ctx context.Context, tx pgx.Tx, actorID string, m organization.Membership, action, oldRole, oldStatus string) error {
	_, err := auditExec(ctx, tx, `INSERT INTO admin_audit_events
        (actor_account_id,organization_id,resource_type,resource_id,action,details)
        VALUES ($1,$2,'membership',$3,$4,
          jsonb_build_object('targetAccountId',$5::text,'oldRole',$6::text,'newRole',$7::text,
            'oldStatus',$8::text,'newStatus',$9::text))`,
		actorID, m.OrganizationID, m.ID, action, m.UserAccountID, oldRole, m.Role, oldStatus, m.Status)
	if err != nil {
		return err
	}
	kind := agentnotification.KindOrganizationMembershipChange
	if action == "membership_invite" {
		kind = agentnotification.KindOrganizationInvitation
	}
	_, err = routeNativeNotification(ctx, tx, kind, m.ID, m.UserAccountID)
	return err
}

func (s *Store) InviteMember(ctx context.Context, actorID, orgID, targetID, role string) (organization.Membership, error) {
	tx, actorRole, err := s.membershipTx(ctx, actorID, orgID)
	if err != nil {
		return organization.Membership{}, err
	}
	defer tx.Rollback(ctx)
	if actorRole != "owner" && actorRole != "admin" {
		return organization.Membership{}, organization.ErrForbidden
	}
	if role != "member" && role != "moderator" && !(actorRole == "owner" && role == "admin") {
		return organization.Membership{}, organization.ErrForbidden
	}
	if targetID == actorID {
		return organization.Membership{}, organization.ErrInvalidTarget
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT account_type='person' AND status='active' FROM accounts WHERE id=$1`, targetID).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) || !eligible {
		return organization.Membership{}, organization.ErrInvalidTarget
	}
	if err != nil {
		return organization.Membership{}, err
	}
	var oldStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM organization_memberships
        WHERE organization_id=$1 AND user_account_id=$2`, orgID, targetID).Scan(&oldStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return organization.Membership{}, err
	}
	if oldStatus != "" && oldStatus != "removed" {
		return organization.Membership{}, organization.ErrConflict
	}
	var membershipID string
	err = tx.QueryRow(ctx, `INSERT INTO organization_memberships
        (organization_id,user_account_id,role,status)
        VALUES ($1,$2,$3,'invited')
        ON CONFLICT (organization_id,user_account_id) DO UPDATE
          SET role=EXCLUDED.role,status='invited',updated_at=now()
          WHERE organization_memberships.status='removed'
		RETURNING id`, orgID, targetID, role).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.Membership{}, organization.ErrConflict
	}
	if err != nil {
		return organization.Membership{}, err
	}
	m, err := readMembership(ctx, tx, membershipID, actorID)
	if err != nil {
		return organization.Membership{}, err
	}
	if err = membershipAudit(ctx, tx, actorID, m, "membership_invite", "", oldStatus); err != nil {
		return organization.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return organization.Membership{}, err
	}
	return m, nil
}

func (s *Store) AcceptInvitation(ctx context.Context, actorID, membershipID string) (organization.Membership, error) {
	var orgID string
	err := s.pool.QueryRow(ctx, `SELECT organization_id FROM organization_memberships WHERE id=$1`, membershipID).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.Membership{}, organization.ErrForbidden
	}
	if err != nil {
		return organization.Membership{}, err
	}
	tx, _, err := s.membershipTxForInvite(ctx, actorID, orgID)
	if err != nil {
		return organization.Membership{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID string
	err = tx.QueryRow(ctx, `UPDATE organization_memberships m
        SET status='active',updated_at=now()
        FROM accounts a
        WHERE m.id=$1 AND m.organization_id=$2 AND m.user_account_id=$3
          AND m.status='invited' AND a.id=$3 AND a.status='active' AND a.account_type='person'
		RETURNING m.id`, membershipID, orgID, actorID).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.Membership{}, organization.ErrForbidden
	}
	if err != nil {
		return organization.Membership{}, err
	}
	m, err := readMembership(ctx, tx, updatedID, actorID)
	if err != nil {
		return organization.Membership{}, err
	}
	if err = membershipAudit(ctx, tx, actorID, m, "membership_accept", m.Role, "invited"); err != nil {
		return organization.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return organization.Membership{}, err
	}
	return m, nil
}

func (s *Store) membershipTxForInvite(ctx context.Context, actorID, orgID string) (pgx.Tx, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, false, err
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT status='active' FROM organizations WHERE id=$1 FOR UPDATE`, orgID).Scan(&active)
	if err != nil || !active {
		tx.Rollback(ctx)
		return nil, false, organization.ErrForbidden
	}
	return tx, true, nil
}

func (s *Store) ChangeMemberRole(ctx context.Context, actorID, orgID, membershipID, newRole string) (organization.Membership, error) {
	tx, actorRole, err := s.membershipTx(ctx, actorID, orgID)
	if err != nil {
		return organization.Membership{}, err
	}
	defer tx.Rollback(ctx)
	if actorRole != "owner" && actorRole != "admin" {
		return organization.Membership{}, organization.ErrForbidden
	}
	var oldRole, status string
	err = tx.QueryRow(ctx, `SELECT role,status FROM organization_memberships WHERE id=$1 AND organization_id=$2 FOR UPDATE`, membershipID, orgID).Scan(&oldRole, &status)
	if errors.Is(err, pgx.ErrNoRows) || status != "active" {
		return organization.Membership{}, organization.ErrForbidden
	}
	if err != nil {
		return organization.Membership{}, err
	}
	if newRole == oldRole {
		return organization.Membership{}, organization.ErrConflict
	}
	if actorRole == "admin" && (oldRole == "owner" || oldRole == "admin" || newRole == "owner" || newRole == "admin") {
		return organization.Membership{}, organization.ErrForbidden
	}
	if newRole != "member" && newRole != "moderator" && !(actorRole == "owner" && (newRole == "admin" || newRole == "owner")) {
		return organization.Membership{}, organization.ErrForbidden
	}
	if oldRole == "owner" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships WHERE organization_id=$1 AND role='owner' AND status='active'`, orgID).Scan(&count); err != nil {
			return organization.Membership{}, err
		}
		if count <= 1 {
			return organization.Membership{}, organization.ErrConflict
		}
	}
	var updatedID string
	err = tx.QueryRow(ctx, `UPDATE organization_memberships m SET role=$3,updated_at=now()
		WHERE m.id=$1 AND m.organization_id=$2 RETURNING m.id`, membershipID, orgID, newRole).Scan(&updatedID)
	if err != nil {
		return organization.Membership{}, err
	}
	m, err := readMembership(ctx, tx, updatedID, actorID)
	if err != nil {
		return organization.Membership{}, err
	}
	if err = membershipAudit(ctx, tx, actorID, m, "membership_role_change", oldRole, status); err != nil {
		return organization.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return organization.Membership{}, err
	}
	return m, nil
}

func (s *Store) RevokeMember(ctx context.Context, actorID, orgID, membershipID string) error {
	tx, actorRole, err := s.membershipTx(ctx, actorID, orgID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if actorRole != "owner" && actorRole != "admin" {
		return organization.ErrForbidden
	}
	var oldRole, status string
	err = tx.QueryRow(ctx, `SELECT role,status FROM organization_memberships WHERE id=$1 AND organization_id=$2 FOR UPDATE`, membershipID, orgID).Scan(&oldRole, &status)
	if errors.Is(err, pgx.ErrNoRows) || status == "removed" {
		return organization.ErrForbidden
	}
	if err != nil {
		return err
	}
	if actorRole == "admin" && (oldRole == "owner" || oldRole == "admin") {
		return organization.ErrForbidden
	}
	if oldRole == "owner" && status == "active" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships WHERE organization_id=$1 AND role='owner' AND status='active'`, orgID).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return organization.ErrConflict
		}
	}
	var updatedID string
	err = tx.QueryRow(ctx, `UPDATE organization_memberships m
        SET status='removed',updated_at=now() WHERE m.id=$1 AND m.organization_id=$2
		RETURNING m.id`, membershipID, orgID).Scan(&updatedID)
	if err != nil {
		return err
	}
	m, err := readMembership(ctx, tx, updatedID, actorID)
	if err != nil {
		return err
	}
	if err = membershipAudit(ctx, tx, actorID, m, "membership_revoke", oldRole, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
