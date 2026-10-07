package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ businessconsole.Store = (*Store)(nil)

type businessBinding struct {
	access         businessconsole.Access
	principalID    string
	role           string
	permissions    []string
	mode           string
	fresh          bool
	approvalSource string
}

func businessError(e error) error {
	for _, known := range []error{businessconsole.ErrInvalid, businessconsole.ErrForbidden, businessconsole.ErrConflict, businessconsole.ErrNotFound, businessconsole.ErrUnavailable, identity.ErrUnauthorized} {
		if errors.Is(e, known) {
			return known
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return businessconsole.ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "40001" || pg.Code == "40P01") {
		return businessconsole.ErrConflict
	}
	return businessconsole.ErrUnavailable
}
func businessSessionError(e error) error {
	if errors.Is(e, identity.ErrUnauthorized) {
		return e
	}
	if errors.Is(e, content.ErrUnavailable) {
		return businessconsole.ErrUnavailable
	}
	return businessError(e)
}

// The domain takes Business before its existing publisher-compatible account
// and membership locks. Session is last, and never refreshed by this Store.
// No Activity, public Venue or Agent is changed while these locks are held.
func (s *Store) businessBegin(ctx context.Context, a businessconsole.Access, mode string, extraAccounts ...string) (pgx.Tx, businessBinding, error) {
	return s.businessBeginWithSource(ctx, a, mode, "", "", extraAccounts...)
}
func (s *Store) businessBeginReview(ctx context.Context, a businessconsole.Access, mode string, decision string, place string) (pgx.Tx, businessBinding, error) {
	if decision != "approve" {
		return s.businessBegin(ctx, a, mode)
	}
	return s.businessBeginWithSource(ctx, a, mode, mode, place)
}
func (s *Store) businessBeginWithSource(ctx context.Context, a businessconsole.Access, mode string, sourceKind string, sourcePlace string, extraAccounts ...string) (pgx.Tx, businessBinding, error) {
	b := businessBinding{access: a, mode: mode, permissions: []string{}}
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || businessconsole.ValidateAccess(a, true) != nil {
		return nil, b, businessconsole.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, businessconsole.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, businessBinding, error) {
		_ = tx.Rollback(context.Background())
		return nil, b, businessError(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	// Missing rows cannot be locked: serialize this stable Business ID before
	// initial creation. It is only a lock key, never proof of ownership.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,70003))`, a.BusinessID); e != nil {
		return fail(e)
	}
	var status string
	e = tx.QueryRow(ctx, `SELECT account_id,status FROM businesses WHERE id=$1 FOR NO KEY UPDATE`, a.BusinessID).Scan(&b.principalID, &status)
	if errors.Is(e, pgx.ErrNoRows) && mode == "create" {
		b.fresh = true
	} else if e != nil {
		return fail(e)
	} else if status != "active" {
		return fail(businessconsole.ErrForbidden)
	}
	// Positive review binds the precise current submission while the stable
	// Business lock is held, before sorted Account and permission locks and
	// the final Session lock. Rejection/revocation remains available even if
	// the former submitter has lost authority.
	if sourceKind != "" {
		var query string
		switch sourceKind {
		case "claim":
			query = `SELECT submitted_by FROM business_claim_controls WHERE business_id=$1 FOR SHARE`
		case "profile":
			query = `SELECT submitted_by FROM business_console_profiles WHERE business_id=$1 FOR SHARE`
		case "venue":
			query = `SELECT submitted_by FROM business_console_venue_facts WHERE business_id=$1 AND place_id=$2 FOR SHARE`
		default:
			return fail(businessconsole.ErrInvalid)
		}
		if sourceKind == "venue" {
			e = tx.QueryRow(ctx, query, a.BusinessID, sourcePlace).Scan(&b.approvalSource)
		} else {
			e = tx.QueryRow(ctx, query, a.BusinessID).Scan(&b.approvalSource)
		}
		if e != nil {
			return fail(e)
		}
		extraAccounts = append(extraAccounts, b.approvalSource)
	}
	ids := append([]string{a.ActingPersonID}, extraAccounts...)
	if b.principalID != "" {
		ids = append(ids, b.principalID)
	}
	for _, id := range ids {
		if !businessconsole.ValidID(id) {
			return fail(businessconsole.ErrInvalid)
		}
	}
	rows, e := tx.Query(ctx, `SELECT id,account_type,status FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, ids)
	if e != nil {
		return fail(e)
	}
	found := map[string]bool{}
	for rows.Next() {
		var id, kind, state string
		if e = rows.Scan(&id, &kind, &state); e != nil {
			rows.Close()
			return fail(e)
		}
		removalTarget := mode == "remove" && id != a.ActingPersonID && id != b.principalID && kind == "person"
		if (state == "active" || removalTarget) && ((id == b.principalID && kind == "business") || (id != b.principalID && kind == "person")) {
			found[id] = true
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return fail(e)
	}
	for _, id := range ids {
		if !found[id] {
			return fail(businessconsole.ErrForbidden)
		}
	}
	if !b.fresh {
		var membershipStatus string
		e = tx.QueryRow(ctx, `SELECT role,status FROM business_memberships WHERE business_id=$1 AND user_account_id=$2 FOR SHARE`, a.BusinessID, a.ActingPersonID).Scan(&b.role, &membershipStatus)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return fail(e)
		}
		if membershipStatus != "active" {
			b.role = ""
		}
		var grantStatus string
		e = tx.QueryRow(ctx, `SELECT permissions,state FROM business_review_grants WHERE business_id=$1 AND reviewer_account_id=$2 FOR SHARE`, a.BusinessID, a.ActingPersonID).Scan(&b.permissions, &grantStatus)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return fail(e)
		}
		if grantStatus != "active" {
			b.permissions = []string{}
		}
		// Current Business members do not independently certify their own
		// merchant, even if trusted operations accidentally provision a grant.
		if b.role != "" {
			b.permissions = []string{}
		}
		switch mode {
		case "manage", "create", "remove":
			if b.role != "owner" && b.role != "admin" {
				return fail(businessconsole.ErrForbidden)
			}
		case "owner":
			if b.role != "owner" {
				return fail(businessconsole.ErrForbidden)
			}
		case "claim", "profile", "venue":
			if !slices.Contains(b.permissions, mode) {
				return fail(businessconsole.ErrForbidden)
			}
		case "read":
			if b.role != "owner" && b.role != "admin" && len(b.permissions) == 0 {
				return fail(businessconsole.ErrForbidden)
			}
		default:
			return fail(businessconsole.ErrForbidden)
		}
	}
	if b.approvalSource != "" {
		var sourceRole, sourceStatus string
		e = tx.QueryRow(ctx, `SELECT role,status FROM business_memberships WHERE business_id=$1 AND user_account_id=$2 FOR SHARE`, a.BusinessID, b.approvalSource).Scan(&sourceRole, &sourceStatus)
		if e != nil || sourceStatus != "active" || (sourceRole != "owner" && sourceRole != "admin") {
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return fail(e)
			}
			return fail(businessconsole.ErrForbidden)
		}
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID); e != nil {
		return fail(businessSessionError(e))
	}
	if _, e = s.businessBoundary(ctx, tx, b, nil); e != nil {
		return fail(e)
	}
	return tx, b, nil
}
func (s *Store) businessBoundary(ctx context.Context, tx pgx.Tx, b businessBinding, validUntil *time.Time) (time.Time, error) {
	if e := checkHumanMomentSession(ctx, tx, b.access.SessionDigest, b.access.ActingPersonID, s.devPhoneEnabled); e != nil {
		return time.Time{}, businessSessionError(e)
	}
	var now time.Time
	var current, deadline bool
	if b.fresh {
		e := tx.QueryRow(ctx, `SELECT clock_timestamp(),EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND account_type='person' AND status='active')`, b.access.ActingPersonID).Scan(&now, &current)
		if e != nil || !current {
			return time.Time{}, businessconsole.ErrForbidden
		}
		return now, nil
	}
	// A new statement clock follows every resource/audit wait. The same locked
	// role/grant and actual Session must still satisfy the current boundary.
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT stamp.at,EXISTS(SELECT 1 FROM businesses b JOIN accounts principal ON principal.id=b.account_id
 JOIN accounts actor ON actor.id=$2 JOIN sessions se ON se.account_id=actor.id
 WHERE b.id=$1 AND b.account_id=$3 AND b.status='active' AND principal.account_type='business' AND principal.status='active'
 AND actor.account_type='person' AND actor.status='active' AND se.token_sha256=$4 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ($5::boolean OR se.authentication_method<>'dev_phone')
 AND (($6 IN ('manage','create','owner','remove') AND EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=b.id AND m.user_account_id=actor.id AND m.status='active' AND ((m.role IN ('owner','admin') AND $6<>'owner') OR m.role='owner')))
 OR ($6 IN ('claim','profile','venue','read') AND (
 ($6='read' AND EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=b.id AND m.user_account_id=actor.id AND m.status='active' AND m.role IN ('owner','admin'))) OR
 EXISTS(SELECT 1 FROM business_review_grants g WHERE g.business_id=b.id AND g.reviewer_account_id=actor.id AND g.state='active' AND g.valid_from<=stamp.at AND g.valid_until>stamp.at AND ($6='read' OR $6=ANY(g.permissions))
 AND NOT EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=b.id AND m.user_account_id=actor.id AND m.status='active')))))),
 ($7::timestamptz IS NULL OR $7>stamp.at) FROM stamp`, b.access.BusinessID, b.access.ActingPersonID, b.principalID, b.access.SessionDigest[:], s.devPhoneEnabled, b.mode, validUntil).Scan(&now, &current, &deadline)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, businessconsole.ErrUnavailable
	}
	if !current {
		return time.Time{}, businessconsole.ErrForbidden
	}
	if !deadline {
		return time.Time{}, businessconsole.ErrConflict
	}
	if b.approvalSource != "" {
		var sourceCurrent bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts person JOIN business_memberships m ON m.user_account_id=person.id WHERE person.id=$1 AND person.account_type='person' AND person.status='active' AND m.business_id=$2 AND m.status='active' AND m.role IN ('owner','admin'))`, b.approvalSource, b.access.BusinessID).Scan(&sourceCurrent)
		if e != nil {
			return time.Time{}, businessconsole.ErrUnavailable
		}
		if !sourceCurrent {
			return time.Time{}, businessconsole.ErrForbidden
		}
	}
	return now, nil
}
func businessAudit(ctx context.Context, tx pgx.Tx, b businessBinding, action, resource string, version int64) error {
	_, e := auditExec(ctx, tx, `INSERT INTO business_console_audit_events(business_id,actor_account_id,action,resource_id,resource_version) VALUES($1,$2,$3,$4,$5)`, b.access.BusinessID, b.access.ActingPersonID, action, resource, version)
	return businessErrorOrNil(e)
}
func businessErrorOrNil(e error) error {
	if e == nil {
		return nil
	}
	return businessError(e)
}
func (s *Store) businessFinish(ctx context.Context, tx pgx.Tx, b businessBinding, deadline *time.Time) error {
	if _, e := s.businessBoundary(ctx, tx, b, deadline); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return businessError(e)
	}
	if ctx.Err() != nil {
		return businessconsole.ErrUnavailable
	}
	return nil
}
func (s *Store) ValidateBusinessAccess(ctx context.Context, a businessconsole.Access) (time.Time, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return time.Time{}, businessconsole.ErrUnavailable
	}
	if a.BusinessID != "" {
		tx, b, e := s.businessBegin(ctx, a, "read")
		if e != nil {
			return time.Time{}, e
		}
		defer tx.Rollback(context.Background())
		now, e := s.businessBoundary(ctx, tx, b, nil)
		if e != nil {
			return time.Time{}, e
		}
		return now, s.businessFinish(ctx, tx, b, nil)
	}
	if businessconsole.ValidateAccess(a, false) != nil {
		return time.Time{}, businessconsole.ErrForbidden
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return time.Time{}, businessconsole.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ActingPersonID).Scan(&id)
	if e != nil {
		return time.Time{}, businessconsole.ErrForbidden
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID); e != nil {
		return time.Time{}, businessSessionError(e)
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID, s.devPhoneEnabled); e != nil {
		return time.Time{}, businessSessionError(e)
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return time.Time{}, businessconsole.ErrUnavailable
	}
	return now, businessErrorOrNil(tx.Commit(ctx))
}

const businessClaimColumns = `version,state,source_url,rights_note,submitted_by,reviewed_by,reviewed_at,review_note,name,description`

func scanBusinessClaim(row pgx.Row) (businessconsole.Claim, error) {
	var c businessconsole.Claim
	e := row.Scan(&c.Version, &c.State, &c.SourceURL, &c.RightsNote, &c.SubmittedBy, &c.ReviewedBy, &c.ReviewedAt, &c.ReviewNote, &c.Name, &c.Description)
	return c, e
}
func (s *Store) SubmitBusinessClaim(ctx context.Context, a businessconsole.Access, in businessconsole.ClaimInput) (businessconsole.Claim, error) {
	if businessconsole.ValidateClaim(in) != nil {
		return businessconsole.Claim{}, businessconsole.ErrInvalid
	}
	tx, b, e := s.businessBegin(ctx, a, "create")
	if e != nil {
		return businessconsole.Claim{}, e
	}
	defer tx.Rollback(context.Background())
	if b.fresh {
		if in.ExpectedVersion != 0 {
			return businessconsole.Claim{}, businessconsole.ErrConflict
		}
		e = tx.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'business') RETURNING id`).Scan(&b.principalID)
		if e != nil {
			return businessconsole.Claim{}, businessError(e)
		}
		_, e = tx.Exec(ctx, `INSERT INTO businesses(id,account_id,name,description,claim_status) VALUES($1,$2,$3,$4,'pending')`, a.BusinessID, b.principalID, in.Name, in.Description)
		if e != nil {
			return businessconsole.Claim{}, businessError(e)
		}
		_, e = tx.Exec(ctx, `INSERT INTO business_memberships(business_id,user_account_id,role,status) VALUES($1,$2,'owner','active')`, a.BusinessID, a.ActingPersonID)
		if e != nil {
			return businessconsole.Claim{}, businessError(e)
		}
		b.fresh = false
		b.role = "owner"
	}
	c, e := scanBusinessClaim(tx.QueryRow(ctx, `SELECT `+businessClaimColumns+` FROM business_claim_controls WHERE business_id=$1 FOR UPDATE`, a.BusinessID))
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return c, businessError(e)
	}
	if e == nil && c.Version == in.ExpectedVersion+1 && c.State == "pending" && c.SubmittedBy == a.ActingPersonID && c.Name == in.Name && c.Description == in.Description && c.SourceURL == in.SourceURL && c.RightsNote == in.RightsNote {
		return c, s.businessFinish(ctx, tx, b, nil)
	}
	if (e == nil && (c.Version != in.ExpectedVersion || c.State == "verified")) || (errors.Is(e, pgx.ErrNoRows) && in.ExpectedVersion != 0) {
		return c, businessconsole.ErrConflict
	}
	c, e = scanBusinessClaim(tx.QueryRow(ctx, `INSERT INTO business_claim_controls(business_id,version,submitted_by,name,description,source_url,rights_note,state)
 VALUES($1,1,$2,$3,$4,$5,$6,'pending') ON CONFLICT(business_id) DO UPDATE SET version=business_claim_controls.version+1,submitted_by=EXCLUDED.submitted_by,name=EXCLUDED.name,description=EXCLUDED.description,source_url=EXCLUDED.source_url,rights_note=EXCLUDED.rights_note,state='pending',reviewed_by=NULL,reviewed_at=NULL,review_note=''
 RETURNING `+businessClaimColumns, a.BusinessID, a.ActingPersonID, in.Name, in.Description, in.SourceURL, in.RightsNote))
	if e != nil {
		return c, businessError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE businesses SET claim_status='pending',claim_source_url=NULL,claim_reviewed_by=NULL,claim_reviewed_at=NULL,updated_at=clock_timestamp() WHERE id=$1`, a.BusinessID)
	if e != nil {
		return c, businessError(e)
	}
	if e = businessAudit(ctx, tx, b, "claim_submit", a.BusinessID, c.Version); e != nil {
		return c, e
	}
	return c, s.businessFinish(ctx, tx, b, nil)
}
func reviewState(decision string) string {
	switch decision {
	case "approve":
		return "verified"
	case "reject":
		return "rejected"
	default:
		return "revoked"
	}
}
func businessReviewAllowed(state string, in businessconsole.ReviewInput, submittedBy, reviewer string) error {
	if submittedBy == reviewer {
		return businessconsole.ErrForbidden
	}
	if (in.Decision == "revoke" && state != "verified") || (in.Decision != "revoke" && state != "pending") {
		return businessconsole.ErrConflict
	}
	return nil
}
func (s *Store) ReviewBusinessClaim(ctx context.Context, a businessconsole.Access, in businessconsole.ReviewInput) (businessconsole.Claim, error) {
	if businessconsole.ValidateReview(in) != nil {
		return businessconsole.Claim{}, businessconsole.ErrInvalid
	}
	tx, b, e := s.businessBeginReview(ctx, a, "claim", in.Decision, "")
	if e != nil {
		return businessconsole.Claim{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := scanBusinessClaim(tx.QueryRow(ctx, `SELECT `+businessClaimColumns+` FROM business_claim_controls WHERE business_id=$1 FOR UPDATE`, a.BusinessID))
	if e != nil {
		return c, businessError(e)
	}
	if c.Version == in.ExpectedVersion+1 && c.State == reviewState(in.Decision) && c.ReviewedBy != nil && *c.ReviewedBy == a.ActingPersonID && c.ReviewNote == in.Note {
		return c, s.businessFinish(ctx, tx, b, nil)
	}
	if c.Version != in.ExpectedVersion {
		return c, businessconsole.ErrConflict
	}
	if e = businessReviewAllowed(c.State, in, c.SubmittedBy, a.ActingPersonID); e != nil {
		return c, e
	}
	c, e = scanBusinessClaim(tx.QueryRow(ctx, `UPDATE business_claim_controls SET version=version+1,state=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),review_note=$4 WHERE business_id=$1 RETURNING `+businessClaimColumns, a.BusinessID, reviewState(in.Decision), a.ActingPersonID, in.Note))
	if e != nil {
		return c, businessError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE businesses SET claim_status=$2,claim_source_url=$3,claim_reviewed_by=$4,claim_reviewed_at=$5,name=CASE WHEN $2='verified' THEN $6 ELSE name END,description=CASE WHEN $2='verified' THEN $7 ELSE description END,updated_at=clock_timestamp() WHERE id=$1`, a.BusinessID, c.State, c.SourceURL, c.ReviewedBy, c.ReviewedAt, c.Name, c.Description)
	if e != nil {
		return c, businessError(e)
	}
	if e = businessAudit(ctx, tx, b, "claim_"+in.Decision, a.BusinessID, c.Version); e != nil {
		return c, e
	}
	// The original claim review, audit, attention decision and Inbox row commit
	// together. A missing current recipient creates no cached notification.
	if _, e = routeNativeNotification(ctx, tx, agentnotification.KindBusinessClaimReview, a.BusinessID, c.SubmittedBy); e != nil {
		return c, businessError(e)
	}
	return c, s.businessFinish(ctx, tx, b, nil)
}

const businessProfileColumns = `version,state,source_url,rights_note,submitted_by,reviewed_by,reviewed_at,review_note,facts,valid_until`

func scanBusinessProfile(row pgx.Row) (businessconsole.Profile, error) {
	var p businessconsole.Profile
	var raw []byte
	e := row.Scan(&p.Version, &p.State, &p.SourceURL, &p.RightsNote, &p.SubmittedBy, &p.ReviewedBy, &p.ReviewedAt, &p.ReviewNote, &raw, &p.ValidUntil)
	if e == nil && (json.Unmarshal(raw, &p.Facts) != nil || businessconsole.ValidateProfile(p.Facts) != nil) {
		e = businessconsole.ErrUnavailable
	}
	return p, e
}
func (s *Store) PutBusinessProfile(ctx context.Context, a businessconsole.Access, in businessconsole.ProfileInput) (businessconsole.Profile, error) {
	in.ValidUntil = in.ValidUntil.UTC().Truncate(time.Microsecond)
	tx, b, e := s.businessBegin(ctx, a, "manage")
	if e != nil {
		return businessconsole.Profile{}, e
	}
	defer tx.Rollback(context.Background())
	now, e := s.businessBoundary(ctx, tx, b, nil)
	if e != nil {
		return businessconsole.Profile{}, e
	}
	if businessconsole.ValidateProfileInput(in, now) != nil {
		return businessconsole.Profile{}, businessconsole.ErrInvalid
	}
	p, e := scanBusinessProfile(tx.QueryRow(ctx, `SELECT `+businessProfileColumns+` FROM business_console_profiles WHERE business_id=$1 FOR UPDATE`, a.BusinessID))
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return p, businessError(e)
	}
	if e == nil && p.Version == in.ExpectedVersion+1 && p.State == "pending" && p.SubmittedBy == a.ActingPersonID && reflect.DeepEqual(p.Facts, in.Facts) && p.SourceURL == in.SourceURL && p.RightsNote == in.RightsNote && p.ValidUntil.Equal(in.ValidUntil) {
		return p, s.businessFinish(ctx, tx, b, &in.ValidUntil)
	}
	if (e == nil && p.Version != in.ExpectedVersion) || (errors.Is(e, pgx.ErrNoRows) && in.ExpectedVersion != 0) {
		return p, businessconsole.ErrConflict
	}
	raw, e := json.Marshal(in.Facts)
	if e != nil {
		return p, businessconsole.ErrInvalid
	}
	p, e = scanBusinessProfile(tx.QueryRow(ctx, `INSERT INTO business_console_profiles(business_id,version,submitted_by,facts,source_url,rights_note,valid_until,state) VALUES($1,1,$2,$3,$4,$5,$6,'pending') ON CONFLICT(business_id) DO UPDATE SET version=business_console_profiles.version+1,submitted_by=EXCLUDED.submitted_by,facts=EXCLUDED.facts,source_url=EXCLUDED.source_url,rights_note=EXCLUDED.rights_note,valid_until=EXCLUDED.valid_until,state='pending',reviewed_by=NULL,reviewed_at=NULL,review_note='' RETURNING `+businessProfileColumns, a.BusinessID, a.ActingPersonID, raw, in.SourceURL, in.RightsNote, in.ValidUntil))
	if e != nil {
		return p, businessError(e)
	}
	if e = businessAudit(ctx, tx, b, "profile_submit", a.BusinessID, p.Version); e != nil {
		return p, e
	}
	return p, s.businessFinish(ctx, tx, b, &in.ValidUntil)
}
func (s *Store) ReviewBusinessProfile(ctx context.Context, a businessconsole.Access, in businessconsole.ReviewInput) (businessconsole.Profile, error) {
	if businessconsole.ValidateReview(in) != nil {
		return businessconsole.Profile{}, businessconsole.ErrInvalid
	}
	tx, b, e := s.businessBeginReview(ctx, a, "profile", in.Decision, "")
	if e != nil {
		return businessconsole.Profile{}, e
	}
	defer tx.Rollback(context.Background())
	if in.Decision == "approve" {
		if e = checkBusinessClaimVerified(ctx, tx, a.BusinessID); e != nil {
			return businessconsole.Profile{}, e
		}
	}
	p, e := scanBusinessProfile(tx.QueryRow(ctx, `SELECT `+businessProfileColumns+` FROM business_console_profiles WHERE business_id=$1 FOR UPDATE`, a.BusinessID))
	if e != nil {
		return p, businessError(e)
	}
	deadline := (*time.Time)(nil)
	if in.Decision == "approve" {
		deadline = &p.ValidUntil
	}
	if p.Version == in.ExpectedVersion+1 && p.State == reviewState(in.Decision) && p.ReviewedBy != nil && *p.ReviewedBy == a.ActingPersonID && p.ReviewNote == in.Note {
		return p, s.businessFinish(ctx, tx, b, deadline)
	}
	if p.Version != in.ExpectedVersion {
		return p, businessconsole.ErrConflict
	}
	if e = businessReviewAllowed(p.State, in, p.SubmittedBy, a.ActingPersonID); e != nil {
		return p, e
	}
	if _, e = s.businessBoundary(ctx, tx, b, deadline); e != nil {
		return p, e
	}
	p, e = scanBusinessProfile(tx.QueryRow(ctx, `UPDATE business_console_profiles SET version=version+1,state=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),review_note=$4 WHERE business_id=$1 RETURNING `+businessProfileColumns, a.BusinessID, reviewState(in.Decision), a.ActingPersonID, in.Note))
	if e != nil {
		return p, businessError(e)
	}
	// Only the reviewed profile can replace these original public identity
	// labels. Editing pending facts never retains a verified badge or silently
	// changes the original merchant name/description.
	if in.Decision == "approve" {
		_, e = tx.Exec(ctx, `UPDATE businesses SET name=$2,description=$3,updated_at=clock_timestamp() WHERE id=$1`, a.BusinessID, p.Facts.Name, p.Facts.Description)
		if e != nil {
			return p, businessError(e)
		}
	}
	if e = businessAudit(ctx, tx, b, "profile_"+in.Decision, a.BusinessID, p.Version); e != nil {
		return p, e
	}
	return p, s.businessFinish(ctx, tx, b, deadline)
}

const businessVenueColumns = `f.version,f.state,f.source_url,f.rights_note,f.submitted_by,f.reviewed_by,f.reviewed_at,f.review_note,f.facts,f.valid_until,f.place_id,p.name,coalesce(r.status,'unsubmitted')`

func scanBusinessVenue(row pgx.Row) (businessconsole.Venue, error) {
	var v businessconsole.Venue
	var raw []byte
	e := row.Scan(&v.Version, &v.State, &v.SourceURL, &v.RightsNote, &v.SubmittedBy, &v.ReviewedBy, &v.ReviewedAt, &v.ReviewNote, &raw, &v.ValidUntil, &v.PlaceID, &v.PlaceName, &v.OperationStatus)
	if e == nil && (json.Unmarshal(raw, &v.Facts) != nil || businessconsole.ValidateVenueFacts(v.Facts) != nil) {
		e = businessconsole.ErrUnavailable
	}
	return v, e
}
func getBusinessVenue(ctx context.Context, tx pgx.Tx, a businessconsole.Access, place string) (businessconsole.Venue, error) {
	return scanBusinessVenue(tx.QueryRow(ctx, `SELECT `+businessVenueColumns+` FROM business_console_venue_facts f JOIN places p ON p.id=f.place_id LEFT JOIN business_venue_relations r ON r.business_id=f.business_id AND r.place_id=f.place_id WHERE f.business_id=$1 AND f.place_id=$2 FOR UPDATE OF f`, a.BusinessID, place))
}

// The side facts do not change public040 Venue facts. Only an independent
// current reviewer may certify this Business's separate operation relation.
func lockBusinessVenueSource(ctx context.Context, tx pgx.Tx, place string) error {
	var id string
	var operator *string
	e := tx.QueryRow(ctx, `SELECT p.id,v.operator_organization_id FROM venues v JOIN venue_candidates vc ON vc.id=v.source_candidate_id JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id JOIN cities c ON c.id=p.city_id
 LEFT JOIN organizations o ON o.id=v.operator_organization_id
 WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published' AND vc.status='approved'
 AND vc.place_id=p.id AND vc.city_id=c.id AND vc.reviewed_by=v.reviewed_by
 AND (v.operator_organization_id IS NULL OR o.status='active')
 AND v.expires_at>clock_timestamp() AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 FOR SHARE OF v,vc,p,c`, place).Scan(&id, &operator)
	if errors.Is(e, pgx.ErrNoRows) {
		return businessconsole.ErrForbidden
	}
	if e != nil {
		return businessError(e)
	}
	// The locked Venue fixes the optional operator ID. Lock that existing
	// Organization separately: PostgreSQL cannot SHARE-lock a nullable join.
	if operator != nil {
		e = tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id=$1 AND status='active' FOR SHARE`, *operator).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return businessconsole.ErrForbidden
		}
		if e != nil {
			return businessError(e)
		}
	}
	return checkBusinessVenueSource(ctx, tx, place)
}
func checkBusinessVenueSource(ctx context.Context, tx pgx.Tx, place string) error {
	var ok bool
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) SELECT EXISTS(SELECT 1 FROM venues v JOIN venue_candidates vc ON vc.id=v.source_candidate_id JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id JOIN cities c ON c.id=p.city_id LEFT JOIN organizations o ON o.id=v.operator_organization_id,stamp
 WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published' AND vc.status='approved' AND vc.place_id=p.id AND vc.city_id=c.id AND vc.reviewed_by=v.reviewed_by AND (v.operator_organization_id IS NULL OR o.status='active') AND v.expires_at>stamp.at AND (p.expires_at IS NULL OR p.expires_at>stamp.at) AND (c.expires_at IS NULL OR c.expires_at>stamp.at))`, place).Scan(&ok)
	if e != nil {
		return businessconsole.ErrUnavailable
	}
	if !ok {
		return businessconsole.ErrForbidden
	}
	return nil
}
func checkBusinessClaimVerified(ctx context.Context, tx pgx.Tx, business string) error {
	var ok bool
	e := tx.QueryRow(ctx, `SELECT claim_status='verified' AND claim_source_url IS NOT NULL AND claim_reviewed_by IS NOT NULL AND claim_reviewed_at IS NOT NULL FROM businesses WHERE id=$1`, business).Scan(&ok)
	if e != nil {
		return businessconsole.ErrUnavailable
	}
	if !ok {
		return businessconsole.ErrForbidden
	}
	return nil
}
func (s *Store) PutBusinessVenueFacts(ctx context.Context, a businessconsole.Access, place string, in businessconsole.VenueInput) (businessconsole.Venue, error) {
	if !businessconsole.ValidID(place) {
		return businessconsole.Venue{}, businessconsole.ErrInvalid
	}
	in.ValidUntil = in.ValidUntil.UTC().Truncate(time.Microsecond)
	tx, b, e := s.businessBegin(ctx, a, "manage")
	if e != nil {
		return businessconsole.Venue{}, e
	}
	defer tx.Rollback(context.Background())
	now, e := s.businessBoundary(ctx, tx, b, nil)
	if e != nil {
		return businessconsole.Venue{}, e
	}
	if businessconsole.ValidateVenueInput(in, now) != nil {
		return businessconsole.Venue{}, businessconsole.ErrInvalid
	}
	if e = checkBusinessClaimVerified(ctx, tx, a.BusinessID); e != nil {
		return businessconsole.Venue{}, e
	}
	if e = lockBusinessVenueSource(ctx, tx, place); e != nil {
		return businessconsole.Venue{}, e
	}
	v, e := getBusinessVenue(ctx, tx, a, place)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, businessError(e)
	}
	if e == nil && v.Version == in.ExpectedVersion+1 && v.State == "pending" && v.SubmittedBy == a.ActingPersonID && reflect.DeepEqual(v.Facts, in.Facts) && v.SourceURL == in.SourceURL && v.RightsNote == in.RightsNote && v.ValidUntil.Equal(in.ValidUntil) {
		if e = checkBusinessVenueSource(ctx, tx, place); e != nil {
			return v, e
		}
		return v, s.businessFinish(ctx, tx, b, &in.ValidUntil)
	}
	if (e == nil && v.Version != in.ExpectedVersion) || (errors.Is(e, pgx.ErrNoRows) && in.ExpectedVersion != 0) {
		return v, businessconsole.ErrConflict
	}
	raw, e := json.Marshal(in.Facts)
	if e != nil {
		return v, businessconsole.ErrInvalid
	}
	_, e = tx.Exec(ctx, `INSERT INTO business_console_venue_facts(business_id,place_id,version,submitted_by,facts,source_url,rights_note,valid_until,state) VALUES($1,$2,1,$3,$4,$5,$6,$7,'pending') ON CONFLICT(business_id,place_id) DO UPDATE SET version=business_console_venue_facts.version+1,submitted_by=EXCLUDED.submitted_by,facts=EXCLUDED.facts,source_url=EXCLUDED.source_url,rights_note=EXCLUDED.rights_note,valid_until=EXCLUDED.valid_until,state='pending',reviewed_by=NULL,reviewed_at=NULL,review_note=''`, a.BusinessID, place, a.ActingPersonID, raw, in.SourceURL, in.RightsNote, in.ValidUntil)
	if e != nil {
		return v, businessError(e)
	}
	// A pending replacement changes no previously verified operating right.
	// Its own facts have no verified stamp until independently reviewed.
	_, e = tx.Exec(ctx, `INSERT INTO business_venue_relations(business_id,place_id,evidence_url,submitted_by,status) VALUES($1,$2,$3,$4,'pending') ON CONFLICT(business_id,place_id) DO NOTHING`, a.BusinessID, place, in.SourceURL, a.ActingPersonID)
	if e != nil {
		return v, businessError(e)
	}
	v, e = getBusinessVenue(ctx, tx, a, place)
	if e != nil {
		return v, businessError(e)
	}
	if e = businessAudit(ctx, tx, b, "venue_submit", place, v.Version); e != nil {
		return v, e
	}
	if e = checkBusinessVenueSource(ctx, tx, place); e != nil {
		return v, e
	}
	return v, s.businessFinish(ctx, tx, b, &in.ValidUntil)
}
func (s *Store) ReviewBusinessVenueFacts(ctx context.Context, a businessconsole.Access, place string, in businessconsole.ReviewInput) (businessconsole.Venue, error) {
	if !businessconsole.ValidID(place) || businessconsole.ValidateReview(in) != nil {
		return businessconsole.Venue{}, businessconsole.ErrInvalid
	}
	tx, b, e := s.businessBeginReview(ctx, a, "venue", in.Decision, place)
	if e != nil {
		return businessconsole.Venue{}, e
	}
	defer tx.Rollback(context.Background())
	if in.Decision == "approve" {
		if e = checkBusinessClaimVerified(ctx, tx, a.BusinessID); e != nil {
			return businessconsole.Venue{}, e
		}
		if e = lockBusinessVenueSource(ctx, tx, place); e != nil {
			return businessconsole.Venue{}, e
		}
	}
	v, e := getBusinessVenue(ctx, tx, a, place)
	if e != nil {
		return v, businessError(e)
	}
	deadline := (*time.Time)(nil)
	if in.Decision == "approve" {
		deadline = &v.ValidUntil
	}
	if v.Version == in.ExpectedVersion+1 && v.State == reviewState(in.Decision) && v.ReviewedBy != nil && *v.ReviewedBy == a.ActingPersonID && v.ReviewNote == in.Note {
		if in.Decision == "approve" {
			if e = checkBusinessVenueSource(ctx, tx, place); e != nil {
				return v, e
			}
		}
		return v, s.businessFinish(ctx, tx, b, deadline)
	}
	if v.Version != in.ExpectedVersion {
		return v, businessconsole.ErrConflict
	}
	if e = businessReviewAllowed(v.State, in, v.SubmittedBy, a.ActingPersonID); e != nil {
		return v, e
	}
	if _, e = s.businessBoundary(ctx, tx, b, deadline); e != nil {
		return v, e
	}
	_, e = tx.Exec(ctx, `UPDATE business_console_venue_facts SET version=version+1,state=$3,reviewed_by=$4,reviewed_at=clock_timestamp(),review_note=$5 WHERE business_id=$1 AND place_id=$2`, a.BusinessID, place, reviewState(in.Decision), a.ActingPersonID, in.Note)
	if e != nil {
		return v, businessError(e)
	}
	// Rejection/revocation never certifies operating rights. Explicit revoke
	// invalidates the existing original041 relation and public eligibility.
	if in.Decision == "approve" || in.Decision == "revoke" {
		state := "verified"
		if in.Decision == "revoke" {
			state = "revoked"
		}
		_, e = tx.Exec(ctx, `UPDATE business_venue_relations SET status=$3,evidence_url=$4,submitted_by=$5,reviewed_by=$6,reviewed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE business_id=$1 AND place_id=$2`, a.BusinessID, place, state, v.SourceURL, v.SubmittedBy, a.ActingPersonID)
		if e != nil {
			return v, businessError(e)
		}
	}
	v, e = getBusinessVenue(ctx, tx, a, place)
	if e != nil {
		return v, businessError(e)
	}
	if e = businessAudit(ctx, tx, b, "venue_"+in.Decision, place, v.Version); e != nil {
		return v, e
	}
	if in.Decision == "approve" {
		if e = checkBusinessVenueSource(ctx, tx, place); e != nil {
			return v, e
		}
	}
	return v, s.businessFinish(ctx, tx, b, deadline)
}

func readBusinessConsoleTx(ctx context.Context, tx pgx.Tx, b businessBinding) (businessconsole.Console, error) {
	out := businessconsole.Console{Venues: []businessconsole.Venue{}, Members: []businessconsole.Member{}, ReviewPermissions: append([]string{}, b.permissions...)}
	out.CanManage = b.role == "owner" || b.role == "admin"
	out.CanManageMembers = b.role == "owner"
	out.CanReview = len(b.permissions) > 0
	out.Business.Role = b.role
	if out.CanReview {
		out.Business.Role = "reviewer"
	}
	e := tx.QueryRow(ctx, `SELECT id,name,claim_status FROM businesses WHERE id=$1`, b.access.BusinessID).Scan(&out.Business.ID, &out.Business.Name, &out.Business.ClaimStatus)
	if e != nil {
		return out, businessError(e)
	}
	if out.CanManage || slices.Contains(b.permissions, "claim") {
		c, e := scanBusinessClaim(tx.QueryRow(ctx, `SELECT `+businessClaimColumns+` FROM business_claim_controls WHERE business_id=$1 FOR SHARE`, b.access.BusinessID))
		if e == nil {
			out.Claim = &c
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return out, businessError(e)
		}
	}
	if out.CanManage || slices.Contains(b.permissions, "profile") {
		p, e := scanBusinessProfile(tx.QueryRow(ctx, `SELECT `+businessProfileColumns+` FROM business_console_profiles WHERE business_id=$1 FOR SHARE`, b.access.BusinessID))
		if e == nil {
			out.Profile = &p
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return out, businessError(e)
		}
	}
	if out.CanManage || slices.Contains(b.permissions, "venue") {
		rows, e := tx.Query(ctx, `SELECT `+businessVenueColumns+` FROM business_console_venue_facts f JOIN places p ON p.id=f.place_id LEFT JOIN business_venue_relations r ON r.business_id=f.business_id AND r.place_id=f.place_id WHERE f.business_id=$1 ORDER BY f.place_id LIMIT 101 FOR SHARE OF f`, b.access.BusinessID)
		if e != nil {
			return out, businessError(e)
		}
		for rows.Next() {
			v, e := scanBusinessVenue(rows)
			if e != nil {
				rows.Close()
				return out, businessError(e)
			}
			out.Venues = append(out.Venues, v)
		}
		rows.Close()
		if rows.Err() != nil || len(out.Venues) > 100 {
			return out, businessconsole.ErrUnavailable
		}
	}
	// Independent reviewers see only facts in their granted scope, and never
	// the merchant's membership roster or private unrelated review materials.
	if !out.CanManage {
		if !slices.Contains(b.permissions, "claim") {
			out.Claim = nil
		}
		if !slices.Contains(b.permissions, "profile") {
			out.Profile = nil
		}
		if !slices.Contains(b.permissions, "venue") {
			out.Venues = []businessconsole.Venue{}
		}
		return out, nil
	}
	e = tx.QueryRow(ctx, `SELECT version FROM business_console_membership_controls WHERE business_id=$1 FOR SHARE`, b.access.BusinessID).Scan(&out.MembershipVersion)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, businessError(e)
	}
	rows, e := tx.Query(ctx, `SELECT user_account_id,role,status FROM business_memberships WHERE business_id=$1 ORDER BY user_account_id LIMIT 101 FOR SHARE`, b.access.BusinessID)
	if e != nil {
		return out, businessError(e)
	}
	for rows.Next() {
		var m businessconsole.Member
		if e = rows.Scan(&m.PersonID, &m.Role, &m.Status); e != nil {
			rows.Close()
			return out, businessError(e)
		}
		out.Members = append(out.Members, m)
	}
	rows.Close()
	if rows.Err() != nil || len(out.Members) > 100 {
		return out, businessconsole.ErrUnavailable
	}
	return out, nil
}
func (s *Store) ReadBusinessConsole(ctx context.Context, a businessconsole.Access) (businessconsole.Console, error) {
	tx, b, e := s.businessBegin(ctx, a, "read")
	if e != nil {
		return businessconsole.Console{}, e
	}
	defer tx.Rollback(context.Background())
	out, e := readBusinessConsoleTx(ctx, tx, b)
	if e != nil {
		return out, e
	}
	now, e := s.businessBoundary(ctx, tx, b, nil)
	if e != nil {
		return out, e
	}
	if out.Profile != nil && !out.Profile.ValidUntil.After(now) && out.Profile.State == "verified" {
		out.Profile.State = "expired"
	}
	for i := range out.Venues {
		if !out.Venues[i].ValidUntil.After(now) && out.Venues[i].State == "verified" {
			out.Venues[i].State = "expired"
		}
	}
	return out, s.businessFinish(ctx, tx, b, nil)
}
func (s *Store) ListBusinessConsoles(ctx context.Context, a businessconsole.Access) ([]businessconsole.Business, error) {
	if ctx == nil || s == nil || s.pool == nil || businessconsole.ValidateAccess(a, false) != nil || a.BusinessID != "" {
		return nil, businessconsole.ErrForbidden
	}
	if _, e := s.ValidateBusinessAccess(ctx, a); e != nil {
		return nil, e
	}
	// Only bounded IDs are collected here; every returned resource is locked
	// and reauthorized by ReadBusinessConsole before any fact can be emitted.
	rows, e := s.pool.Query(ctx, `SELECT DISTINCT b.id FROM businesses b JOIN accounts principal ON principal.id=b.account_id
 WHERE b.status='active' AND principal.status='active' AND principal.account_type='business' AND (
 EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=b.id AND m.user_account_id=$1 AND m.status='active' AND m.role IN ('owner','admin')) OR
 EXISTS(SELECT 1 FROM business_review_grants g WHERE g.business_id=b.id AND g.reviewer_account_id=$1 AND g.state='active' AND g.valid_from<=clock_timestamp() AND g.valid_until>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=b.id AND m.user_account_id=$1 AND m.status='active')))
 ORDER BY b.id LIMIT 101`, a.ActingPersonID)
	if e != nil {
		return nil, businessError(e)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, businessError(e)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil || len(ids) > businessconsole.MaxBusinesses {
		return nil, businessconsole.ErrUnavailable
	}
	out := []businessconsole.Business{}
	for _, id := range ids {
		single := a
		single.BusinessID = id
		console, e := s.ReadBusinessConsole(ctx, single)
		if e != nil {
			return nil, e
		}
		out = append(out, console.Business)
	}
	// Earlier per-resource transactions have committed. A later resource may
	// have waited while an earlier grant/role was revoked. Re-project all list
	// facts in one current statement after those waits; checking only Session
	// here would leak the earlier resource. No subsequent resource lock wait
	// or Session refresh occurs after this boundary.
	var authorized bool
	var raw []byte
	e = s.pool.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at),
 authorized AS MATERIALIZED(SELECT EXISTS(SELECT 1 FROM accounts actor JOIN sessions se ON se.account_id=actor.id,stamp WHERE actor.id=$1 AND actor.account_type='person' AND actor.status='active' AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ($3::boolean OR se.authentication_method<>'dev_phone')) AS ok),
 items AS (SELECT b.id,b.name,b.claim_status AS "claimStatus",coalesce(m.role,'reviewer') AS role
 FROM businesses b JOIN accounts principal ON principal.id=b.account_id
 LEFT JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=$1 AND m.status='active' AND m.role IN ('owner','admin')
 LEFT JOIN business_review_grants g ON g.business_id=b.id AND g.reviewer_account_id=$1,stamp,authorized
 WHERE authorized.ok AND b.status='active' AND principal.account_type='business' AND principal.status='active'
 AND (m.role IS NOT NULL OR (g.state='active' AND g.valid_from<=stamp.at AND g.valid_until>stamp.at AND NOT EXISTS(SELECT 1 FROM business_memberships member WHERE member.business_id=b.id AND member.user_account_id=$1 AND member.status='active')))
 ORDER BY b.id LIMIT 101)
 SELECT authorized.ok,coalesce((SELECT jsonb_agg(to_jsonb(items) ORDER BY id) FROM items),'[]'::jsonb) FROM authorized`, a.ActingPersonID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&authorized, &raw)
	if e != nil {
		return nil, businessError(e)
	}
	if !authorized {
		return nil, identity.ErrUnauthorized
	}
	current := []businessconsole.Business{}
	if json.Unmarshal(raw, &current) != nil || len(current) > businessconsole.MaxBusinesses {
		return nil, businessconsole.ErrUnavailable
	}
	if !reflect.DeepEqual(current, out) {
		return nil, businessconsole.ErrForbidden
	}
	return current, nil
}
func (s *Store) ChangeBusinessMember(ctx context.Context, a businessconsole.Access, in businessconsole.MemberInput) (businessconsole.Console, error) {
	if businessconsole.ValidateMember(in) != nil || in.TargetPersonID == a.ActingPersonID {
		return businessconsole.Console{}, businessconsole.ErrInvalid
	}
	mode := "manage"
	if in.Action == "remove" {
		mode = "remove"
	}
	tx, b, e := s.businessBegin(ctx, a, mode, in.TargetPersonID)
	if e != nil {
		return businessconsole.Console{}, e
	}
	defer tx.Rollback(context.Background())
	var version int64
	e = tx.QueryRow(ctx, `SELECT version FROM business_console_membership_controls WHERE business_id=$1 FOR UPDATE`, a.BusinessID).Scan(&version)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return businessconsole.Console{}, businessError(e)
	}
	var role, status string
	e = tx.QueryRow(ctx, `SELECT role,status FROM business_memberships WHERE business_id=$1 AND user_account_id=$2 FOR UPDATE`, a.BusinessID, in.TargetPersonID).Scan(&role, &status)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return businessconsole.Console{}, businessError(e)
	}
	// A transfer may have committed before its response was lost. The former
	// owner is now only admin: permit resolution of that exact durable outcome
	// without restoring any owner power or replaying a fresh mutation.
	transferReplay := in.Action == "transfer_owner" && b.role == "admin" && version == in.ExpectedVersion+1 && role == "owner" && status == "active"
	if b.role != "owner" && !transferReplay {
		return businessconsole.Console{}, businessconsole.ErrForbidden
	}
	// Repeated membership mutation is recognized by its exact durable audit
	// and resulting target state; it cannot replay a different role/target.
	if version == in.ExpectedVersion+1 && ((in.Action == "grant" && role == in.Role && status == "active") || (in.Action == "remove" && status == "removed") || transferReplay) {
		var already bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_console_audit_events WHERE business_id=$1 AND actor_account_id=$2 AND action=$3 AND resource_id=$4 AND resource_version=$5)`, a.BusinessID, a.ActingPersonID, "member_"+in.Action, in.TargetPersonID, version).Scan(&already)
		if e != nil {
			return businessconsole.Console{}, businessError(e)
		}
		if already {
			out, e := readBusinessConsoleTx(ctx, tx, b)
			if e != nil {
				return out, e
			}
			return out, s.businessFinish(ctx, tx, b, nil)
		}
	}
	if b.role != "owner" || (role == "owner" && status == "active") {
		return businessconsole.Console{}, businessconsole.ErrForbidden
	}
	if version != in.ExpectedVersion {
		return businessconsole.Console{}, businessconsole.ErrConflict
	}
	switch in.Action {
	case "grant":
		_, e = tx.Exec(ctx, `INSERT INTO business_memberships(business_id,user_account_id,role,status) VALUES($1,$2,$3,'active') ON CONFLICT(business_id,user_account_id) DO UPDATE SET role=EXCLUDED.role,status='active',updated_at=clock_timestamp()`, a.BusinessID, in.TargetPersonID, in.Role)
	case "remove":
		if status != "active" {
			return businessconsole.Console{}, businessconsole.ErrConflict
		}
		_, e = tx.Exec(ctx, `UPDATE business_memberships SET status='removed',updated_at=clock_timestamp() WHERE business_id=$1 AND user_account_id=$2`, a.BusinessID, in.TargetPersonID)
	case "transfer_owner":
		if status != "active" || (role != "admin" && role != "member") {
			return businessconsole.Console{}, businessconsole.ErrConflict
		}
		// Both updates are atomic. The original partial unique index continues
		// to enforce at most one owner; this path preserves exactly one.
		_, e = tx.Exec(ctx, `UPDATE business_memberships SET role='admin',updated_at=clock_timestamp() WHERE business_id=$1 AND user_account_id=$2 AND role='owner' AND status='active'`, a.BusinessID, a.ActingPersonID)
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE business_memberships SET role='owner',updated_at=clock_timestamp() WHERE business_id=$1 AND user_account_id=$2`, a.BusinessID, in.TargetPersonID)
		}
	}
	if e != nil {
		return businessconsole.Console{}, businessError(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO business_console_membership_controls(business_id,version) VALUES($1,1) ON CONFLICT(business_id) DO UPDATE SET version=business_console_membership_controls.version+1`, a.BusinessID)
	if e != nil {
		return businessconsole.Console{}, businessError(e)
	}
	if e = businessAudit(ctx, tx, b, "member_"+in.Action, in.TargetPersonID, version+1); e != nil {
		return businessconsole.Console{}, e
	}
	if in.Action == "transfer_owner" {
		b.mode = "manage"
		b.role = "admin"
	}
	out, e := readBusinessConsoleTx(ctx, tx, b)
	if e != nil {
		return out, e
	}
	return out, s.businessFinish(ctx, tx, b, nil)
}
