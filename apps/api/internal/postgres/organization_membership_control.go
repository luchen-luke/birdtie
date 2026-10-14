package postgres

import (
	"context"
	"errors"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

func (s *Store) ReadMembersControl(ctx context.Context, a org.HumanAccess, id string) (org.MembersControl, error) {
	f, e := s.beginOrgControl(ctx, a, id, "", false, false)
	if e != nil {
		return org.MembersControl{}, e
	}
	defer f.tx.Rollback(context.Background())
	if !orgManager(f.role) {
		return org.MembersControl{}, org.ErrForbidden
	}
	out := org.MembersControl{OrganizationID: id, AuthorityVersion: f.authority, Role: f.role, Members: []org.Membership{}, ObservedAt: time.Now().UTC()}
	out.ObservedAt, out.ExpiresAt, e = orgControlTimes(ctx, f, nil)
	if e != nil {
		return out, e
	}
	rows, e := f.tx.Query(ctx, `SELECT `+membershipColumns+` FROM organization_memberships m LEFT JOIN user_profiles p ON p.account_id=m.user_account_id WHERE m.organization_id=$1 ORDER BY m.created_at,m.id`, id, a.AccountID)
	if e != nil {
		return out, orgControlError(e)
	}
	for rows.Next() {
		m, e := scanMembership(rows)
		if e != nil {
			rows.Close()
			return out, orgControlError(e)
		}
		out.Members = append(out.Members, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, orgControlError(e)
	}
	if e = s.orgControlGate(ctx, f, true); e != nil {
		return out, e
	}
	if e = orgControlLease(ctx, f.tx, out.ExpiresAt); e != nil {
		return out, e
	}
	out.Proof = f.proof()
	return out, nil
}
func (s *Store) ReadInvitationsControl(ctx context.Context, a org.HumanAccess) (org.InvitationsControl, error) {
	out := org.InvitationsControl{Items: []org.Membership{}, Proofs: map[string]string{}, ExpiresAt: time.Now().UTC().Add(30 * time.Second)}
	proof, e := s.CaptureOrganizationControlSession(ctx, a)
	if e != nil {
		return out, e
	}
	out.SessionProof = proof
	ids, e := s.pool.Query(ctx, `SELECT DISTINCT m.organization_id FROM organization_memberships m JOIN organizations o ON o.id=m.organization_id AND o.status='active' WHERE m.user_account_id=$1 AND m.status='invited' ORDER BY m.organization_id`, a.AccountID)
	if e != nil {
		return out, orgControlError(e)
	}
	list := []string{}
	for ids.Next() {
		var id string
		if e = ids.Scan(&id); e != nil {
			ids.Close()
			return out, orgControlError(e)
		}
		list = append(list, id)
	}
	e = ids.Err()
	ids.Close()
	if e != nil {
		return out, orgControlError(e)
	}
	for _, id := range list {
		f, e := s.beginOrgControl(ctx, a, id, "", false, false)
		if e != nil {
			return out, e
		}
		if f.session+":"+strconv.FormatInt(f.generation, 10) != proof {
			f.tx.Rollback(context.Background())
			return out, org.ErrControlChanged
		}
		out.Proofs[id] = f.proof()
		f.tx.Rollback(context.Background())
	}
	out.Items, e = s.ListInvitations(ctx, a.AccountID)
	if e != nil {
		return out, orgControlError(e)
	}
	for _, m := range out.Items {
		if _, ok := out.Proofs[m.OrganizationID]; !ok {
			return out, org.ErrControlChanged
		}
	}
	for id, p := range out.Proofs {
		if e = s.ValidateOrganizationControlResponse(ctx, a, id, p, out.ExpiresAt); e != nil {
			return out, e
		}
	}
	if e = s.ValidateOrganizationControlSession(ctx, a, proof); e != nil {
		return out, e
	}
	return out, nil
}
func orgMember(ctx context.Context, f orgControlFence, in org.ControlInput, write bool) (org.Membership, error) {
	var m org.Membership
	tail := ""
	if write {
		tail = " FOR UPDATE"
	}
	q := `SELECT id,organization_id,user_account_id,role,status,control_version FROM organization_memberships WHERE organization_id=$1 AND id=$2` + tail
	target := in.MembershipID
	if in.Action == "INVITE" {
		q = `SELECT id,organization_id,user_account_id,role,status,control_version FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2` + tail
		target = in.UserAccountID
	}
	e := f.tx.QueryRow(ctx, q, f.id, target).Scan(&m.ID, &m.OrganizationID, &m.UserAccountID, &m.Role, &m.Status, &m.Version)
	if errors.Is(e, pgx.ErrNoRows) && in.Action == "INVITE" {
		return org.Membership{OrganizationID: f.id, UserAccountID: target}, nil
	}
	return m, orgControlError(e)
}
func (s *Store) checkOrgMember(ctx context.Context, f orgControlFence, in org.ControlInput, write bool) error {
	if in.Action != "ACCEPT" && !orgManager(f.role) {
		return org.ErrForbidden
	}
	m, e := orgMember(ctx, f, in, write)
	if e != nil {
		return e
	}
	if m.Version != in.ExpectedMemberVersion || m.Version >= org.MaxControlVersion {
		return org.ErrControlChanged
	}
	if in.Action == "INVITE" {
		if in.UserAccountID == f.access.AccountID {
			return org.ErrInvalidTarget
		}
		if m.Status != "" && m.Status != "removed" {
			return org.ErrControlChanged
		}
		if in.Role == "admin" && f.role != "owner" {
			return org.ErrForbidden
		}
		var ok bool
		tail := ""
		if write {
			tail = " FOR SHARE"
		}
		e = f.tx.QueryRow(ctx, `SELECT account_type='person' AND status='active' FROM accounts WHERE id=$1`+tail, in.UserAccountID).Scan(&ok)
		if errors.Is(e, pgx.ErrNoRows) || !ok {
			return org.ErrInvalidTarget
		}
		return orgControlError(e)
	}
	if in.Action == "ACCEPT" {
		if m.UserAccountID != f.access.AccountID || m.Status != "invited" {
			return org.ErrForbidden
		}
		return nil
	}
	if m.Status == "removed" || in.Action == "ROLE" && m.Status != "active" {
		return org.ErrControlChanged
	}
	if f.role == "admin" && (m.Role == "owner" || m.Role == "admin" || in.Role == "owner" || in.Role == "admin") {
		return org.ErrForbidden
	}
	if in.Action == "ROLE" && m.Role == in.Role {
		return org.ErrControlChanged
	}
	var targetActive bool
	if e = f.tx.QueryRow(ctx, `SELECT status='active' AND account_type='person' FROM accounts WHERE id=$1`, m.UserAccountID).Scan(&targetActive); e != nil {
		return orgControlError(e)
	}
	if in.Action == "ROLE" && !targetActive {
		return org.ErrForbidden
	}
	if m.Role == "owner" && m.Status == "active" && targetActive {
		var n int
		e = f.tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships m JOIN accounts a ON a.id=m.user_account_id AND a.status='active' AND a.account_type='person' WHERE m.organization_id=$1 AND m.role='owner' AND m.status='active'`, f.id).Scan(&n)
		if e != nil {
			return orgControlError(e)
		}
		if n <= 1 {
			return org.ErrConflict
		}
	}
	return nil
}
func (s *Store) applyOrgMember(ctx context.Context, f orgControlFence, in org.ControlInput, r *org.ControlReceipt) error {
	m, e := orgMember(ctx, f, in, true)
	if e != nil {
		return e
	}
	oldRole, oldStatus := m.Role, m.Status
	action := ""
	switch in.Action {
	case "INVITE":
		action = "membership_invite"
		if m.ID == "" {
			e = f.tx.QueryRow(ctx, `INSERT INTO organization_memberships(id,organization_id,user_account_id,role,status)VALUES($1,$2,$3,$4,'invited')RETURNING id`, in.OperationID, f.id, in.UserAccountID, in.Role).Scan(&m.ID)
		} else {
			_, e = f.tx.Exec(ctx, `UPDATE organization_memberships SET role=$2,status='invited',updated_at=clock_timestamp() WHERE id=$1`, m.ID, in.Role)
		}
	case "ACCEPT":
		action = "membership_accept"
		_, e = f.tx.Exec(ctx, `UPDATE organization_memberships SET status='active',updated_at=clock_timestamp() WHERE id=$1`, m.ID)
	case "ROLE":
		action = "membership_role_change"
		_, e = f.tx.Exec(ctx, `UPDATE organization_memberships SET role=$2,updated_at=clock_timestamp() WHERE id=$1`, m.ID, in.Role)
	case "REVOKE":
		action = "membership_revoke"
		_, e = f.tx.Exec(ctx, `UPDATE organization_memberships SET status='removed',updated_at=clock_timestamp() WHERE id=$1`, m.ID)
	}
	if e != nil {
		return orgControlError(e)
	}
	m, e = readMembership(ctx, f.tx, m.ID, f.access.AccountID)
	if e != nil {
		return orgControlError(e)
	}
	if e = membershipAudit(ctx, f.tx, f.access.AccountID, m, action, oldRole, oldStatus); e != nil {
		return orgControlError(e)
	}
	r.MembershipID = m.ID
	r.MemberVersion = m.Version
	return nil
}
