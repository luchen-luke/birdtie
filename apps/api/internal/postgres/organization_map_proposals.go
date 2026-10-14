package postgres

import (
	"context"
	"errors"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
	"time"
)

func orgReviewer(ctx context.Context, f orgControlFence, city string, lock bool) bool {
	tail := ""
	if lock {
		tail = " FOR SHARE OF m,a,c"
	}
	var id string
	e := f.tx.QueryRow(ctx, `SELECT m.account_id FROM city_editor_memberships m JOIN accounts a ON a.id=m.account_id AND a.account_type='person' AND a.status='active' JOIN cities c ON c.id=m.city_id AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) WHERE m.city_id=$1 AND m.account_id=$2 AND m.role='reviewer' AND m.state='active'`+tail, city, f.access.AccountID).Scan(&id)
	return e == nil
}
func orgProposal(ctx context.Context, f orgControlFence, id string, write bool) (org.MapProposal, error) {
	p := org.MapProposal{}
	tail := ""
	if write {
		tail = " FOR UPDATE"
	}
	e := f.tx.QueryRow(ctx, `SELECT id,organization_id,city_id,latitude,longitude,status,version,canonical_revision,submitted_by FROM organization_map_proposals WHERE id=$1 AND organization_id=$2`+tail, id, f.id).Scan(&p.ID, &p.OrganizationID, &p.CityID, &p.Latitude, &p.Longitude, &p.Status, &p.Version, &p.CanonicalRevision, &p.SubmittedBy)
	return p, orgControlError(e)
}
func (s *Store) ReadMapControl(ctx context.Context, a org.HumanAccess, id string) (org.MapControl, error) {
	f, e := s.beginOrgControl(ctx, a, id, "", false, false)
	if e != nil {
		return org.MapControl{}, e
	}
	defer f.tx.Rollback(context.Background())
	out := org.MapControl{OrganizationID: id, AuthorityVersion: f.authority, Role: f.role, Proposals: []org.MapProposal{}, ObservedAt: time.Now().UTC()}
	out.ObservedAt, out.ExpiresAt, e = orgControlTimes(ctx, f, nil)
	if e != nil {
		return out, e
	}
	var allowed bool
	e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organization_map_proposals p JOIN city_editor_memberships m ON m.city_id=p.city_id AND m.account_id=$2 AND m.state='active' AND m.role='reviewer' JOIN accounts a ON a.id=m.account_id AND a.status='active' AND a.account_type='person' JOIN cities c ON c.id=m.city_id AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) WHERE p.organization_id=$1)`, id, a.AccountID).Scan(&allowed)
	if e != nil {
		return out, orgControlError(e)
	}
	if !orgManager(f.role) && !allowed {
		return out, org.ErrForbidden
	}
	if !orgManager(f.role) {
		out.Role = "reviewer"
	}
	c, e := scanMapLocation(f.tx.QueryRow(ctx, `SELECT `+mapLocationColumns+` FROM organization_map_locations WHERE organization_id=$1 AND ($2::boolean OR visibility='public' AND review_status='approved' AND EXISTS(SELECT 1 FROM organizations WHERE id=$1 AND visibility='public' AND verification_status='verified'))`, id, orgManager(f.role)))
	if e == nil {
		out.Canonical = &c
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, orgControlError(e)
	}
	rows, e := f.tx.Query(ctx, `SELECT id,organization_id,city_id,latitude,longitude,status,version,canonical_revision,submitted_by FROM organization_map_proposals p WHERE organization_id=$1 AND ($2::boolean OR EXISTS(SELECT 1 FROM city_editor_memberships m JOIN accounts a ON a.id=m.account_id AND a.status='active' AND a.account_type='person' JOIN cities c ON c.id=m.city_id AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) WHERE m.city_id=p.city_id AND m.account_id=$3 AND m.state='active' AND m.role='reviewer')) ORDER BY created_at DESC,id LIMIT 20`, id, orgManager(f.role), a.AccountID)
	if e != nil {
		return out, orgControlError(e)
	}
	for rows.Next() {
		p := org.MapProposal{}
		if e = rows.Scan(&p.ID, &p.OrganizationID, &p.CityID, &p.Latitude, &p.Longitude, &p.Status, &p.Version, &p.CanonicalRevision, &p.SubmittedBy); e != nil {
			rows.Close()
			return out, orgControlError(e)
		}
		out.Proposals = append(out.Proposals, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, orgControlError(e)
	}
	for i := range out.Proposals {
		p := &out.Proposals[i]
		review := orgReviewer(ctx, f, p.CityID, false)
		pending := p.Status == "PENDING" && p.Version < org.MaxControlVersion
		p.CanApprove = pending && review && p.SubmittedBy != a.AccountID && p.CanonicalRevision == f.canonical
		p.CanReject = pending && review && p.SubmittedBy != a.AccountID && p.CanonicalRevision == f.canonical
		p.CanWithdraw = pending && orgManager(f.role)
	}
	var deadline *time.Time
	if e = f.tx.QueryRow(ctx, `SELECT min(c.expires_at) FROM organization_map_proposals p JOIN cities c ON c.id=p.city_id WHERE p.organization_id=$1 AND p.status='PENDING' AND c.publication_status='published' AND c.expires_at>$2`, id, out.ObservedAt).Scan(&deadline); e != nil {
		return out, orgControlError(e)
	}
	if deadline != nil && deadline.Before(out.ExpiresAt) {
		out.ExpiresAt = deadline.UTC()
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
func (s *Store) checkOrgMap(ctx context.Context, f orgControlFence, in org.ControlInput, write bool) error {
	if in.Action == "PROPOSE" || in.Action == "HIDE_POINT" {
		if !orgManager(f.role) {
			return org.ErrForbidden
		}
		if f.canonical != in.ExpectedCanonicalRevision || f.canonical >= org.MaxControlVersion {
			return org.ErrControlChanged
		}
	}
	if in.Action == "PROPOSE" {
		var id string
		tail := ""
		if write {
			tail = " FOR SHARE"
		}
		e := f.tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp())`+tail, in.CityID).Scan(&id)
		if e != nil {
			return orgControlError(e)
		}
		return nil
	}
	if in.Action == "HIDE_POINT" {
		if f.canonical < 1 {
			return org.ErrNotFound
		}
		return nil
	}
	p, e := orgProposal(ctx, f, in.ProposalID, write)
	if e != nil {
		return e
	}
	if p.Status != "PENDING" || p.Version != in.ExpectedProposalVersion || p.Version >= org.MaxControlVersion {
		return org.ErrControlChanged
	}
	if in.Action == "WITHDRAW_POINT" {
		if !orgManager(f.role) {
			return org.ErrForbidden
		}
		return nil
	}
	if !orgReviewer(ctx, f, p.CityID, write) {
		return org.ErrForbidden
	}
	if p.SubmittedBy == f.access.AccountID {
		return org.ErrForbidden
	}
	if p.CanonicalRevision != in.ExpectedCanonicalRevision || f.canonical != in.ExpectedCanonicalRevision || f.canonical >= org.MaxControlVersion {
		return org.ErrControlChanged
	}
	return nil
}
func (s *Store) applyOrgMap(ctx context.Context, f orgControlFence, in org.ControlInput, r *org.ControlReceipt) error {
	var e error
	switch in.Action {
	case "PROPOSE":
		_, e = f.tx.Exec(ctx, `INSERT INTO organization_map_proposals(id,organization_id,city_id,latitude,longitude,canonical_revision,submitted_by)VALUES($1,$2,$3,$4,$5,$6,$7)`, in.OperationID, f.id, in.CityID, in.Latitude, in.Longitude, f.canonical, f.access.AccountID)
		r.ProposalID = in.OperationID
		r.ProposalVersion = 1
		r.CanonicalRevision = f.canonical
	case "WITHDRAW_POINT", "REJECT_POINT", "APPROVE_POINT":
		p, err := orgProposal(ctx, f, in.ProposalID, true)
		if err != nil {
			return err
		}
		status := "WITHDRAWN"
		if in.Action == "REJECT_POINT" {
			status = "REJECTED"
		}
		if in.Action == "APPROVE_POINT" {
			status = "APPROVED"
		}
		reviewer := ""
		if status != "WITHDRAWN" {
			reviewer = f.access.AccountID
		}
		e = f.tx.QueryRow(ctx, `UPDATE organization_map_proposals SET status=$2,version=version+1,reviewed_by=nullif($3,'')::uuid WHERE id=$1 RETURNING version`, p.ID, status, reviewer).Scan(&r.ProposalVersion)
		r.ProposalID = p.ID
		r.CanonicalRevision = f.canonical
		if e == nil && in.Action == "APPROVE_POINT" {
			e = f.tx.QueryRow(ctx, `INSERT INTO organization_map_locations(organization_id,city_id,latitude,longitude,visibility,review_status,submitted_by,reviewed_by,reviewed_at,revision)VALUES($1,$2,$3,$4,'public','approved',$5,$6,clock_timestamp(),1) ON CONFLICT(organization_id) DO UPDATE SET city_id=excluded.city_id,latitude=excluded.latitude,longitude=excluded.longitude,visibility='public',review_status='approved',submitted_by=excluded.submitted_by,submitted_at=clock_timestamp(),reviewed_by=excluded.reviewed_by,reviewed_at=clock_timestamp(),review_note='',revision=organization_map_locations.revision+1 RETURNING revision`, f.id, p.CityID, p.Latitude, p.Longitude, p.SubmittedBy, f.access.AccountID).Scan(&r.CanonicalRevision)
			if e == nil {
				_, e = auditExec(ctx, f.tx, `INSERT INTO organization_map_location_audit(organization_id,actor_account_id,action,revision)VALUES($1,$2,'approve',$3)`, f.id, f.access.AccountID, r.CanonicalRevision)
			}
		}
	case "HIDE_POINT":
		e = f.tx.QueryRow(ctx, `UPDATE organization_map_locations SET visibility='hidden',revision=revision+1 WHERE organization_id=$1 RETURNING revision`, f.id).Scan(&r.CanonicalRevision)
		if e == nil {
			_, e = auditExec(ctx, f.tx, `INSERT INTO organization_map_location_audit(organization_id,actor_account_id,action,revision)VALUES($1,$2,'hide',$3)`, f.id, f.access.AccountID, r.CanonicalRevision)
		}
	}
	return orgControlError(e)
}
