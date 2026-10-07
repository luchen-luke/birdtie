package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ agentbusiness.IdentityStore = (*Store)(nil)

// This is deliberately distinct from businessBegin: Agent/Profile locks must
// precede the final native Session lock. The existing runtime remains disabled.
func (s *Store) businessIdentity(ctx context.Context, a businessconsole.Access, expected *int64) (agentbusiness.BusinessIdentity, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return agentbusiness.BusinessIdentity{}, businessconsole.ErrUnavailable
	}
	if businessconsole.ValidateAccess(a, true) != nil {
		return agentbusiness.BusinessIdentity{}, businessconsole.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return agentbusiness.BusinessIdentity{}, businessconsole.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	return s.businessIdentityTx(ctx, tx, a, expected)
}
func (s *Store) ReadBusinessAgentIdentity(ctx context.Context, a businessconsole.Access) (agentbusiness.BusinessIdentity, error) {
	return s.businessIdentity(ctx, a, nil)
}
func (s *Store) EstablishBusinessAgentIdentity(ctx context.Context, a businessconsole.Access, v agentbusiness.EstablishIdentity) (agentbusiness.BusinessIdentity, error) {
	if v.ExpectedClaimVersion < 1 {
		return agentbusiness.BusinessIdentity{}, businessconsole.ErrInvalid
	}
	return s.businessIdentity(ctx, a, &v.ExpectedClaimVersion)
}

const businessIdentityAgentSQL = `SELECT id::text,status,xmin::text FROM agents WHERE agent_type='business' AND principal_account_id=$1 FOR SHARE`
const businessIdentityFinalSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT stamp.at,
 EXISTS(SELECT 1 FROM businesses b JOIN accounts ba ON ba.id=b.account_id JOIN accounts pa ON pa.id=$2
 JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=pa.id
 WHERE b.id=$1 AND b.account_id=$3 AND b.status='active' AND b.xmin::text=$4
 AND ba.account_type='business' AND ba.status='active' AND ba.xmin::text=$5
 AND pa.account_type='person' AND pa.status='active' AND pa.xmin::text=$6
 AND m.status='active' AND m.role=$7 AND m.role IN('owner','admin') AND m.xmin::text=$8
 AND stamp.at<$9 AND
 (($10::bigint=0 AND NOT EXISTS(SELECT 1 FROM business_claim_controls c WHERE c.business_id=b.id)) OR
 EXISTS(SELECT 1 FROM business_claim_controls c WHERE c.business_id=b.id AND c.version=$10 AND c.xmin::text=$11))
 AND (NOT $12::boolean OR (b.claim_status='verified' AND isfinite(b.claim_reviewed_at) AND b.claim_reviewed_at<=stamp.at
 AND EXISTS(SELECT 1 FROM business_claim_controls c WHERE c.business_id=b.id AND c.version=$10 AND c.state='verified' AND isfinite(c.reviewed_at) AND c.reviewed_at<=stamp.at)))
 AND (($13::uuid IS NULL AND NOT EXISTS(SELECT 1 FROM agents g WHERE g.agent_type='business' AND g.principal_account_id=ba.id)) OR
 EXISTS(SELECT 1 FROM agents g JOIN agent_profiles p ON p.agent_id=g.id
 WHERE g.id=$13 AND g.principal_account_id=ba.id AND g.agent_type='business' AND g.status=$14 AND g.status IN('suspended','retired')
 AND g.xmin::text=$15 AND p.owner_type='BUSINESS' AND p.owner_id=ba.id AND p.profile_version=$16 AND p.xmin::text=$17))),
 EXISTS(SELECT 1 FROM sessions se WHERE se.token_sha256=$18 AND se.account_id=$2 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ($19::boolean OR se.authentication_method<>'dev_phone')),
 COALESCE((SELECT se.xmin::text FROM sessions se WHERE se.token_sha256=$18 AND se.account_id=$2),'') FROM stamp`

func (s *Store) businessIdentityTx(ctx context.Context, tx pgx.Tx, a businessconsole.Access, expected *int64) (agentbusiness.BusinessIdentity, error) {
	var v agentbusiness.BusinessIdentity
	fail := func(e error) (agentbusiness.BusinessIdentity, error) {
		return agentbusiness.BusinessIdentity{}, businessError(e)
	}
	if ctx == nil || ctx.Err() != nil || s == nil || tx == nil || businessconsole.ValidateAccess(a, true) != nil {
		return fail(businessconsole.ErrForbidden)
	}
	if _, e := tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,70003))`, a.BusinessID); e != nil {
		return fail(e)
	}
	var businessState, bxmin string
	var started time.Time
	e := tx.QueryRow(ctx, `SELECT account_id::text,name,status,claim_status,xmin::text,clock_timestamp() FROM businesses WHERE id=$1 FOR NO KEY UPDATE`, a.BusinessID).Scan(&v.Principal.ID, &v.BusinessName, &businessState, &v.ClaimStatus, &bxmin, &started)
	if e != nil {
		return fail(e)
	}
	if businessState != "active" || !businessconsole.ValidID(v.Principal.ID) || v.Principal.ID == a.ActingPersonID {
		return fail(businessconsole.ErrForbidden)
	}
	v.Principal.Type = actorref.Business
	v.BusinessID = a.BusinessID
	v.Schema = agentbusiness.IdentitySchema
	v.MetadataOnly = true
	v.Tools = []string{}
	v.ClaimState = "pending"
	v.ValidUntil = started.UTC().Add(30 * time.Second)
	var cxmin string
	e = tx.QueryRow(ctx, `SELECT version,state,xmin::text FROM business_claim_controls WHERE business_id=$1 FOR SHARE`, a.BusinessID).Scan(&v.ClaimVersion, &v.ClaimState, &cxmin)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return fail(e)
	}
	ids := []string{a.ActingPersonID, v.Principal.ID}
	sort.Strings(ids)
	accountVersions := map[string]string{}
	for _, id := range ids {
		var kind, state, xmin string
		if e = tx.QueryRow(ctx, `SELECT account_type,status,xmin::text FROM accounts WHERE id=$1 FOR SHARE`, id).Scan(&kind, &state, &xmin); e != nil {
			return fail(e)
		}
		want := "person"
		if id == v.Principal.ID {
			want = "business"
		}
		if kind != want || state != "active" {
			return fail(businessconsole.ErrForbidden)
		}
		accountVersions[id] = xmin
	}
	var memberState, mxmin string
	if e = tx.QueryRow(ctx, `SELECT role,status,xmin::text FROM business_memberships WHERE business_id=$1 AND user_account_id=$2 FOR SHARE`, a.BusinessID, a.ActingPersonID).Scan(&v.Role, &memberState, &mxmin); e != nil {
		return fail(e)
	}
	if memberState != "active" || (v.Role != "owner" && v.Role != "admin") {
		return fail(businessconsole.ErrForbidden)
	}
	// Permission precedes version conflict disclosure, including stale input.
	if expected != nil {
		if v.ClaimVersion != *expected {
			return fail(businessconsole.ErrConflict)
		}
		if v.ClaimStatus != "verified" || v.ClaimState != "verified" {
			return fail(businessconsole.ErrForbidden)
		}
	}
	var ag agentbusiness.BusinessIdentityAgent
	var gxmin, pxmin string
	created := false
	readAgent := func() error {
		return tx.QueryRow(ctx, businessIdentityAgentSQL, v.Principal.ID).Scan(&ag.ID, &ag.Status, &gxmin)
	}
	e = readAgent()
	if errors.Is(e, pgx.ErrNoRows) && expected != nil {
		// Original053 AFTER INSERT bootstraps its unified Profile in this same Tx.
		e = tx.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('business',$1,'suspended') ON CONFLICT(agent_type,principal_account_id) DO NOTHING RETURNING id::text`, v.Principal.ID).Scan(&ag.ID)
		if e == nil {
			created = true
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return fail(e)
		}
		e = readAgent()
	}
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return fail(e)
	}
	if e == nil {
		if ag.Status != "suspended" && ag.Status != "retired" {
			return fail(businessconsole.ErrUnavailable)
		}
		ag.Type = actorref.Business
		if e = tx.QueryRow(ctx, `SELECT `+agentProfileColumns+`,xmin::text FROM agent_profiles WHERE agent_id=$1 AND owner_type='BUSINESS' AND owner_id=$2 FOR SHARE`, ag.ID, v.Principal.ID).Scan(&ag.Profile.AgentID, &ag.Profile.OwnerType, &ag.Profile.OwnerID, &ag.Profile.ProfileVersion, &ag.Profile.CreatedAt, &ag.Profile.UpdatedAt, &pxmin); e != nil {
			return fail(e)
		}
		if agentprofile.Validate(ag.Profile) != nil || ag.Profile.AgentID != ag.ID || ag.Profile.OwnerID != v.Principal.ID || ag.Profile.OwnerType != actorref.Business {
			return fail(businessconsole.ErrUnavailable)
		}
		v.Agent = &ag
	}
	// No new resource locks after this native session lock. Audit waits are
	// followed by a single compound source/session/deadline statement.
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID); e != nil {
		return agentbusiness.BusinessIdentity{}, businessSessionError(e)
	}
	if created {
		if e = businessAudit(ctx, tx, businessBinding{access: a, principalID: v.Principal.ID, role: v.Role}, "agent_provision", ag.ID, 1); e != nil {
			return fail(e)
		}
	}
	var agentID any
	var profileVersion int64
	if v.Agent != nil {
		agentID = ag.ID
		profileVersion = ag.Profile.ProfileVersion
	}
	var current, session bool
	var sxmin string
	e = tx.QueryRow(ctx, businessIdentityFinalSQL, a.BusinessID, a.ActingPersonID, v.Principal.ID, bxmin, accountVersions[v.Principal.ID], accountVersions[a.ActingPersonID], v.Role, mxmin, v.ValidUntil, v.ClaimVersion, cxmin, expected != nil, agentID, ag.Status, gxmin, profileVersion, pxmin, a.SessionDigest[:], s.devPhoneEnabled).Scan(&v.ObservedAt, &current, &session, &sxmin)
	if e != nil || ctx.Err() != nil {
		return fail(businessconsole.ErrUnavailable)
	}
	if !session {
		return agentbusiness.BusinessIdentity{}, identity.ErrUnauthorized
	}
	if !current {
		return fail(businessconsole.ErrConflict)
	}
	v.ObservedAt = v.ObservedAt.UTC()
	v.SourceVersion = agentbusiness.Digest([]byte(strings.Join([]string{a.BusinessID, a.ActingPersonID, v.Principal.ID, bxmin, cxmin, mxmin, accountVersions[v.Principal.ID], accountVersions[a.ActingPersonID], ag.ID, ag.Status, gxmin, pxmin, sxmin, fmt.Sprint(v.ClaimVersion), fmt.Sprint(profileVersion)}, "|")))
	if agentbusiness.ValidateBusinessIdentity(v) != nil {
		return fail(businessconsole.ErrUnavailable)
	}
	if ctx.Err() != nil {
		return fail(businessconsole.ErrUnavailable)
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return fail(businessconsole.ErrUnavailable)
	}
	return v, nil
}
