package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
)

var _ org.HumanControlStore = (*Store)(nil)

type orgControlFence struct {
	tx                               pgx.Tx
	access                           org.HumanAccess
	id, session, source, role        string
	generation, authority, canonical int64
}

func orgControlError(e error) error {
	for _, x := range []error{org.ErrControlInvalid, org.ErrControlChanged, org.ErrControlUnavailable, org.ErrForbidden, org.ErrConflict, org.ErrInvalidTarget, org.ErrNotFound, identity.ErrUnauthorized} {
		if errors.Is(e, x) {
			return x
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return org.ErrNotFound
	}
	if e != nil {
		return org.ErrControlUnavailable
	}
	return nil
}
func (s *Store) orgControlSession(ctx context.Context, tx pgx.Tx, a org.HumanAccess, lock bool) (string, int64, error) {
	if a.AccountType != "person" || !org.ControlID(a.AccountID) || a.SessionDigest == ([32]byte{}) {
		return "", 0, identity.ErrUnauthorized
	}
	tail := ""
	if lock {
		var account string
		if e := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, a.AccountID).Scan(&account); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return "", 0, identity.ErrUnauthorized
			}
			return "", 0, orgControlError(e)
		}
		tail = " FOR SHARE OF s"
	}
	var id string
	var gen int64
	e := tx.QueryRow(ctx, `SELECT s.id,s.authorization_generation FROM sessions s JOIN accounts a ON a.id=s.account_id WHERE a.id=$1 AND a.status='active' AND a.account_type='person' AND s.token_sha256=$2 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp() AND ($3::boolean OR s.authentication_method<>'dev_phone')`+tail, a.AccountID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&id, &gen)
	if errors.Is(e, pgx.ErrNoRows) {
		e = identity.ErrUnauthorized
	}
	if e == nil && lock {
		return s.orgControlSession(ctx, tx, a, false)
	}
	return id, gen, orgControlError(e)
}
func (s *Store) CaptureOrganizationControlSession(ctx context.Context, a org.HumanAccess) (string, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", org.ErrControlUnavailable
	}
	defer tx.Rollback(context.Background())
	id, g, e := s.orgControlSession(ctx, tx, a, false)
	if e != nil {
		return "", e
	}
	now, ng, e := s.orgControlSession(ctx, tx, a, true)
	if e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			return "", org.ErrControlChanged
		}
		return "", e
	}
	if now != id || ng != g {
		return "", org.ErrControlChanged
	}
	return id + ":" + strconv.FormatInt(g, 10), tx.Commit(ctx)
}
func (s *Store) ValidateOrganizationControlSession(ctx context.Context, a org.HumanAccess, proof string) error {
	p, e := s.CaptureOrganizationControlSession(ctx, a)
	if e != nil {
		return e
	}
	if p != proof || proof == "" {
		return org.ErrControlChanged
	}
	return nil
}
func (s *Store) beginOrgControl(ctx context.Context, a org.HumanAccess, id, op string, write, history bool) (orgControlFence, error) {
	f := orgControlFence{access: a, id: id}
	if !org.ControlID(id) || op != "" && !org.ControlID(op) {
		return f, org.ErrControlInvalid
	}
	var e error
	f.tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return f, org.ErrControlUnavailable
	}
	fail := func(e error) (orgControlFence, error) {
		f.tx.Rollback(context.Background())
		return orgControlFence{}, orgControlError(e)
	}
	// Capture before any advisory/resource wait; restored Sessions cannot replace it.
	f.session, f.generation, e = s.orgControlSession(ctx, f.tx, a, false)
	if e != nil {
		return fail(e)
	}
	if op != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,139))`, a.AccountID+":"+op); e != nil {
			return fail(e)
		}
	}
	if e = s.orgControlOriginalSession(ctx, f, false); e != nil {
		return fail(e)
	}
	tail := " FOR SHARE"
	if write {
		tail = " FOR UPDATE"
	}
	var active bool
	e = f.tx.QueryRow(ctx, `SELECT status='active' FROM organizations WHERE id=$1`+tail, id).Scan(&active)
	if e != nil {
		return fail(e)
	}
	if e = s.orgControlOriginalSession(ctx, f, false); e != nil {
		return fail(e)
	}
	if !history && !active {
		return fail(org.ErrForbidden)
	}
	if !history {
		var principal string
		e = f.tx.QueryRow(ctx, `SELECT a.id FROM accounts a JOIN organizations o ON o.account_id=a.id WHERE o.id=$1 AND a.status='active' AND a.account_type='organization' FOR SHARE OF a`, id).Scan(&principal)
		if errors.Is(e, pgx.ErrNoRows) {
			return fail(org.ErrForbidden)
		}
		if e != nil {
			return fail(e)
		}
	}
	atail := ""
	if write {
		atail = " FOR UPDATE"
	}
	e = f.tx.QueryRow(ctx, `SELECT version FROM organization_human_authority WHERE organization_id=$1`+atail, id).Scan(&f.authority)
	if e != nil {
		return fail(e)
	}
	if e = s.orgControlOriginalSession(ctx, f, false); e != nil {
		return fail(e)
	}
	e = f.tx.QueryRow(ctx, `SELECT role FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2 AND status='active'`, id, a.AccountID).Scan(&f.role)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return fail(e)
	}
	e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT revision FROM organization_map_locations WHERE organization_id=$1),0)`, id).Scan(&f.canonical)
	if e != nil {
		return fail(e)
	}
	f.source, e = orgCurrentSource(ctx, f.tx, id, a.AccountID)
	if e != nil {
		return fail(e)
	}
	return f, nil
}
func orgCurrentSource(ctx context.Context, tx pgx.Tx, id, viewer string) (string, error) {
	var proof string
	e := tx.QueryRow(ctx, `SELECT birdtie_organization_human_source($1)||':'||md5(jsonb_build_array(
 (SELECT jsonb_agg(jsonb_build_array(m.id,CASE WHEN p.visibility='public' AND birdtie_profile_visible_to(m.user_account_id,$2::uuid) AND birdtie_agent_profile_field_allowed(m.user_account_id,$2::uuid,'displayName') THEN p.display_name ELSE '' END) ORDER BY m.id) FROM organization_memberships m LEFT JOIN user_profiles p ON p.account_id=m.user_account_id WHERE m.organization_id=$1),
 (SELECT jsonb_agg(jsonb_build_array(c.id,c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())) ORDER BY c.id) FROM cities c WHERE c.id IN(SELECT city_id FROM organization_map_proposals WHERE organization_id=$1)))::text)`, id, viewer).Scan(&proof)
	return proof, orgControlError(e)
}
func (f orgControlFence) proof() string {
	return f.session + ":" + strconv.FormatInt(f.generation, 10) + "|" + f.source
}

// Once the initial current Session has been captured, losing that exact id/
// generation is a retired operation frame, never a missing organization. The
// first capture keeps the ordinary unauthorized result for invalid sessions.
func (s *Store) orgControlOriginalSession(ctx context.Context, f orgControlFence, lock bool) error {
	id, g, e := s.orgControlSession(ctx, f.tx, f.access, lock)
	if e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			return org.ErrControlChanged
		}
		return e
	}
	if id != f.session || g != f.generation {
		return org.ErrControlChanged
	}
	return nil
}
func (s *Store) orgControlGate(ctx context.Context, f orgControlFence, source bool) error {
	if e := s.orgControlOriginalSession(ctx, f, true); e != nil {
		return e
	}
	if source {
		src, e := orgCurrentSource(ctx, f.tx, f.id, f.access.AccountID)
		if e != nil {
			return e
		}
		if src != f.source {
			return org.ErrControlChanged
		}
	}
	return nil
}
func (s *Store) ValidateOrganizationControlResponse(ctx context.Context, a org.HumanAccess, id, proof string, until time.Time) error {
	if !until.IsZero() && !time.Now().Before(until) {
		return org.ErrControlChanged
	}
	f, e := s.beginOrgControl(ctx, a, id, "", false, true)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	if f.proof() != proof {
		return org.ErrControlChanged
	}
	if e = s.orgControlGate(ctx, f, true); e != nil {
		return e
	}
	return orgControlLease(ctx, f.tx, until)
}
func orgControlLease(ctx context.Context, tx pgx.Tx, until time.Time) error {
	if until.IsZero() {
		return nil
	}
	var current bool
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()<$1`, until).Scan(&current); e != nil {
		return orgControlError(e)
	}
	if !current {
		return org.ErrControlChanged
	}
	return nil
}
func orgManager(role string) bool { return role == "owner" || role == "admin" }
func orgControlTimes(ctx context.Context, f orgControlFence, in *org.ControlInput) (time.Time, time.Time, error) {
	var at, until time.Time
	city := ""
	if in != nil {
		city = in.CityID
		if in.Action == "APPROVE_POINT" || in.Action == "REJECT_POINT" {
			if e := f.tx.QueryRow(ctx, `SELECT city_id FROM organization_map_proposals WHERE id=$1 AND organization_id=$2`, in.ProposalID, f.id).Scan(&city); e != nil {
				return at, until, orgControlError(e)
			}
		}
	}
	e := f.tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) SELECT stamp.at,least(stamp.at+interval '30 seconds',expires_at,idle_expires_at,(SELECT expires_at FROM cities WHERE id=nullif($3,''))) FROM sessions,stamp WHERE id=$1 AND authorization_generation=$2`, f.session, f.generation, city).Scan(&at, &until)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return at, until, org.ErrControlChanged
		}
		return at, until, orgControlError(e)
	}
	if !until.After(at) {
		return at, until, org.ErrControlChanged
	}
	return at.UTC(), until.UTC(), nil
}
func orgReceipt(ctx context.Context, tx pgx.Tx, a org.HumanAccess, id, op string) (org.ControlReceipt, string, error) {
	r := org.ControlReceipt{}
	var sum string
	e := tx.QueryRow(ctx, `SELECT organization_id,operation_id,action,status,authority_version,member_version,canonical_revision,proposal_version,coalesce(membership_id::text,''),coalesce(proposal_id::text,''),request_sha256 FROM organization_human_operations WHERE actor_account_id=$1 AND operation_id=$2`, a.AccountID, op).Scan(&r.OrganizationID, &r.OperationID, &r.Action, &r.Status, &r.AuthorityVersion, &r.MemberVersion, &r.CanonicalRevision, &r.ProposalVersion, &r.MembershipID, &r.ProposalID, &sum)
	if e == nil && r.OrganizationID != id {
		return r, sum, org.ErrControlChanged
	}
	return r, sum, orgControlError(e)
}
func insertOrgReceipt(ctx context.Context, f orgControlFence, r org.ControlReceipt, sum string) error {
	_, e := f.tx.Exec(ctx, `INSERT INTO organization_human_operations(actor_account_id,operation_id,organization_id,request_sha256,action,status,authority_version,member_version,canonical_revision,proposal_version,membership_id,proposal_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,nullif($11,'')::uuid,nullif($12,'')::uuid)`, f.access.AccountID, r.OperationID, f.id, sum, r.Action, r.Status, r.AuthorityVersion, r.MemberVersion, r.CanonicalRevision, r.ProposalVersion, r.MembershipID, r.ProposalID)
	return orgControlError(e)
}
func (s *Store) ReadOrganizationControlOperation(ctx context.Context, a org.HumanAccess, id, op string) (org.ControlReceipt, error) {
	f, e := s.beginOrgControl(ctx, a, id, op, false, true)
	if e != nil {
		return org.ControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, _, e := orgReceipt(ctx, f.tx, a, id, op)
	if e != nil {
		return r, e
	}
	if e = s.orgControlGate(ctx, f, false); e != nil {
		return r, e
	}
	r.Proof = f.proof()
	return r, nil
}
func (s *Store) StopOrganizationControlOperation(ctx context.Context, a org.HumanAccess, id, op string, expected int64) (org.ControlReceipt, error) {
	if expected < 1 || expected > org.MaxControlVersion {
		return org.ControlReceipt{}, org.ErrControlInvalid
	}
	f, e := s.beginOrgControl(ctx, a, id, op, true, true)
	if e != nil {
		return org.ControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, _, e := orgReceipt(ctx, f.tx, a, id, op)
	if e == nil {
		if e = s.orgControlGate(ctx, f, false); e != nil {
			return r, e
		}
		r.Proof = f.proof()
		return r, nil
	}
	if !errors.Is(e, org.ErrNotFound) {
		return r, e
	}
	// Self namespace only. Closing an unknown operation grants no business authority.
	r = org.ControlReceipt{OrganizationID: id, OperationID: op, Action: "STOP", Status: "STOPPED"}
	sum := org.ControlDigest(id, org.ControlInput{Action: "STOP", OperationID: op, ExpectedAuthorityVersion: expected})
	if e = s.orgControlGate(ctx, f, false); e != nil {
		return r, e
	}
	if e = insertOrgReceipt(ctx, f, r, sum); e != nil {
		return r, e
	}
	r.Proof = f.proof()
	return r, orgControlError(f.tx.Commit(ctx))
}
func orgApproval(a org.HumanAccess, material string, start, end int64) string {
	p := strconv.FormatInt(start, 10) + "." + strconv.FormatInt(end, 10)
	h := hmac.New(sha256.New, a.SessionDigest[:])
	h.Write([]byte(material + "\x00" + p))
	return "oh1." + p + "." + hex.EncodeToString(h.Sum(nil))
}
func orgApprovalValid(a org.HumanAccess, material, token string, now time.Time) bool {
	p := strings.Split(token, ".")
	if len(p) != 4 || p[0] != "oh1" {
		return false
	}
	start, e := strconv.ParseInt(p[1], 10, 64)
	if e != nil {
		return false
	}
	end, e := strconv.ParseInt(p[2], 10, 64)
	if e != nil || start > now.UnixNano() || end <= now.UnixNano() || end <= start || end-start > int64(30*time.Second) {
		return false
	}
	return hmac.Equal([]byte(token), []byte(orgApproval(a, material, start, end)))
}
func orgApprovalMaterial(ctx context.Context, f orgControlFence, in org.ControlInput) (string, error) {
	extra := ""
	var e error
	if in.Action == "INVITE" {
		e = f.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1`, in.UserAccountID).Scan(&extra)
	}
	if in.Action == "PROPOSE" {
		e = f.tx.QueryRow(ctx, `SELECT xmin::text FROM cities WHERE id=$1`, in.CityID).Scan(&extra)
	}
	if e != nil {
		return "", orgControlError(e)
	}
	b, _ := json.Marshal([]any{f.access.AccountID, f.id, f.proof(), extra, org.ControlDigest(f.id, in)})
	return string(b), nil
}
func (s *Store) PreviewOrganizationControl(ctx context.Context, a org.HumanAccess, id string, in org.ControlInput) (org.ControlPreview, error) {
	in, e := org.NormalizeControl(in)
	if e != nil {
		return org.ControlPreview{}, e
	}
	f, e := s.beginOrgControl(ctx, a, id, "", false, false)
	if e != nil {
		return org.ControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if e = s.checkOrgAction(ctx, f, in, false); e != nil {
		return org.ControlPreview{}, e
	}
	if e = s.orgControlGate(ctx, f, true); e != nil {
		return org.ControlPreview{}, e
	}
	material, e := orgApprovalMaterial(ctx, f, in)
	if e != nil {
		return org.ControlPreview{}, e
	}
	at, until, e := orgControlTimes(ctx, f, &in)
	if e != nil {
		return org.ControlPreview{}, e
	}
	return org.ControlPreview{OrganizationID: id, OperationID: in.OperationID, Action: in.Action, Approval: orgApproval(a, material, at.UnixNano(), until.UnixNano()), ExpiresAt: until, InputSHA256: org.ControlDigest(id, in), AuthorityVersion: f.authority, MemberVersion: in.ExpectedMemberVersion, CanonicalRevision: f.canonical, ProposalVersion: in.ExpectedProposalVersion, Proof: f.proof()}, nil
}
func (s *Store) ApplyOrganizationControl(ctx context.Context, a org.HumanAccess, id string, in org.ControlInput, approval string) (org.ControlReceipt, error) {
	in, e := org.NormalizeControl(in)
	if e != nil {
		return org.ControlReceipt{}, e
	}
	f, e := s.beginOrgControl(ctx, a, id, in.OperationID, true, true)
	if e != nil {
		return org.ControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, sum, e := orgReceipt(ctx, f.tx, a, id, in.OperationID)
	if e == nil {
		if r.Status == "STOPPED" || sum != org.ControlDigest(id, in) {
			return r, org.ErrControlChanged
		}
		if e = s.orgControlGate(ctx, f, false); e != nil {
			return r, e
		}
		r.Proof = f.proof()
		return r, nil
	}
	if !errors.Is(e, org.ErrNotFound) {
		return r, e
	}
	var principal string
	if e = f.tx.QueryRow(ctx, `SELECT a.id FROM accounts a JOIN organizations o ON o.account_id=a.id WHERE o.id=$1 AND a.status='active' AND a.account_type='organization' FOR SHARE OF a`, id).Scan(&principal); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return r, org.ErrForbidden
		}
		return r, orgControlError(e)
	}
	// Lock all current principal rows before evaluating last actual owner.
	rows, e := f.tx.Query(ctx, `SELECT a.id FROM accounts a JOIN organization_memberships m ON m.user_account_id=a.id WHERE m.organization_id=$1 ORDER BY a.id FOR SHARE OF a,m`, id)
	if e != nil {
		return r, orgControlError(e)
	}
	for rows.Next() {
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, orgControlError(e)
	}
	if e = s.checkOrgAction(ctx, f, in, true); e != nil {
		return r, e
	}
	material, e := orgApprovalMaterial(ctx, f, in)
	if e != nil {
		return r, e
	}
	at, _, e := orgControlTimes(ctx, f, &in)
	if e != nil {
		return r, e
	}
	if !orgApprovalValid(a, material, approval, at) {
		return r, org.ErrControlChanged
	}
	if e = s.orgControlGate(ctx, f, true); e != nil {
		return r, e
	}
	r = org.ControlReceipt{OrganizationID: id, OperationID: in.OperationID, Action: in.Action, Status: "SUCCEEDED"}
	if in.Action == "INVITE" || in.Action == "ACCEPT" || in.Action == "ROLE" || in.Action == "REVOKE" {
		e = s.applyOrgMember(ctx, f, in, &r)
	} else {
		e = s.applyOrgMap(ctx, f, in, &r)
	}
	if e != nil {
		return r, e
	}
	if e = f.tx.QueryRow(ctx, `SELECT version FROM organization_human_authority WHERE organization_id=$1`, id).Scan(&r.AuthorityVersion); e != nil {
		return r, orgControlError(e)
	}
	if e = insertOrgReceipt(ctx, f, r, org.ControlDigest(id, in)); e != nil {
		return r, e
	}
	if e = s.orgControlGate(ctx, f, false); e != nil {
		return r, e
	}
	at, _, e = orgControlTimes(ctx, f, &in)
	if e != nil {
		return r, e
	}
	if !orgApprovalValid(a, material, approval, at) {
		return r, org.ErrControlChanged
	}
	// A committed effect gets its new source seal; original immutable receipt stays unchanged.
	f.source, e = orgCurrentSource(ctx, f.tx, id, a.AccountID)
	if e != nil {
		return r, orgControlError(e)
	}
	r.Proof = f.proof()
	return r, orgControlError(f.tx.Commit(ctx))
}
func (s *Store) checkOrgAction(ctx context.Context, f orgControlFence, in org.ControlInput, write bool) error {
	if f.authority != in.ExpectedAuthorityVersion || f.authority >= org.MaxControlVersion {
		return org.ErrControlChanged
	}
	var active bool
	if e := f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organizations o JOIN accounts a ON a.id=o.account_id AND a.status='active' AND a.account_type='organization' WHERE o.id=$1 AND o.status='active')`, f.id).Scan(&active); e != nil {
		return orgControlError(e)
	}
	if !active {
		return org.ErrForbidden
	}
	if in.Action == "INVITE" || in.Action == "ACCEPT" || in.Action == "ROLE" || in.Action == "REVOKE" {
		return s.checkOrgMember(ctx, f, in, write)
	}
	return s.checkOrgMap(ctx, f, in, write)
}
