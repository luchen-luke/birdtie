package agentmembershipsignal

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type nativeResolver struct {
	pool        *pgxpool.Pool
	store       *postgres.Store
	development bool
}

// All source and authority facts come from the final current SQL statement.
// These fragments only read existing domains, never create roles or grants.
const nativeAuthoritySQL = `WITH clock AS MATERIALIZED (SELECT clock_timestamp() AS checked_at), authority AS (
 SELECT s.id AS session_id, LEAST(s.expires_at,s.idle_expires_at) AS session_limit,
 a.id AS owner_id, ag.id AS agent_id,ag.created_at AS agent_created_at,
 ap.profile_version,ap.created_at AS metadata_created_at,ap.xmin::text AS metadata_row_token,
 ag.xmin::text AS agent_row_token,a.xmin::text AS account_row_token,clock.checked_at
 FROM clock JOIN sessions s ON s.token_sha256=$1
 JOIN accounts a ON a.id=s.account_id AND a.id=$2 AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.id=$3 AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' AND ap.profile_version>0
 WHERE s.revoked_at IS NULL AND s.expires_at>clock.checked_at AND s.idle_expires_at>clock.checked_at
 AND ($4::boolean OR s.authentication_method<>'dev_phone')
 AND isfinite(ap.created_at) AND isfinite(ap.updated_at) AND isfinite(ag.created_at)
 AND ap.created_at<=clock.checked_at AND ap.updated_at<=clock.checked_at AND ag.created_at<=clock.checked_at) `
const nativeColumns = `SELECT auth.checked_at,auth.session_limit,
 jsonb_build_object('session',auth.session_id,'agent',auth.agent_id,'agentCreated',auth.agent_created_at,
 'metadataCreated',auth.metadata_created_at,'metadataVersion',auth.profile_version,
 'metadataRowToken',auth.metadata_row_token,'agentRowToken',auth.agent_row_token,'accountRowToken',auth.account_row_token)::text,
 jsonb_build_object('membershipId',m.id,'ownerId',m.user_account_id,'role',m.role,'status',m.status,
 'createdAt',m.created_at,'updatedAt',m.updated_at,'memberRowToken',m.xmin::text, `
const memberTimes = ` AND isfinite(m.created_at) AND isfinite(m.updated_at)
 AND m.created_at>= '0001-01-01 00:00:00+00'::timestamptz AND m.updated_at>=m.created_at AND m.updated_at<=auth.checked_at `

func nativeSQL(kind Kind) (string, error) {
	switch kind {
	case Community:
		return nativeAuthoritySQL + nativeColumns + `
 'resourceId',c.id,'principalAccountId','','label',c.name,'resourceCreatedAt',c.created_at,
 'resourceUpdatedAt',c.updated_at,'resourceExpiresAt',c.expires_at,'resourceVerifiedAt',c.verified_at,
 'publication',c.publication_status,'lifecycle',c.lifecycle_status,'visibility',c.visibility,
 'resourceRowToken',c.xmin::text,'ownerIdAccount',co.id,'ownerRowToken',co.xmin::text,
 'ownerMembershipToken',om.xmin::text,'creatorId',creator.id,'creatorToken',creator.xmin::text,'cityId',city.id,'cityUpdatedAt',city.updated_at,'cityExpiresAt',city.expires_at,
 'cityRowToken',city.xmin::text,'cityContextRowToken',cc.xmin::text)::text,
 LEAST(c.expires_at,city.expires_at)
 FROM authority auth JOIN community_memberships m ON m.id=$5 AND m.user_account_id=auth.owner_id AND m.status='active'
 JOIN communities c ON c.id=m.community_id AND c.publication_status='published' AND c.lifecycle_status='active'
 AND c.visibility IN ('public','private')
 JOIN accounts creator ON creator.id=c.owner_account_id AND creator.status='active' AND creator.account_type='person'
 JOIN community_memberships om ON om.community_id=c.id AND om.role='owner' AND om.status='active'
 JOIN accounts co ON co.id=om.user_account_id AND co.status='active' AND co.account_type='person'
 LEFT JOIN cities city ON city.id=c.city_id LEFT JOIN city_contexts cc ON cc.city_id=city.id
 WHERE m.role IN ('owner','admin','member') AND (SELECT count(*) FROM community_memberships owners WHERE owners.community_id=c.id AND owners.role='owner' AND owners.status='active')=1 AND (c.expires_at IS NULL OR c.expires_at>auth.checked_at)
 AND (c.expires_at IS NULL OR isfinite(c.expires_at))
 AND (c.verified_at IS NULL OR (isfinite(c.verified_at) AND c.verified_at>= '0001-01-01 00:00:00+00'::timestamptz AND c.verified_at<=auth.checked_at))
 AND isfinite(c.created_at) AND isfinite(c.updated_at) AND c.updated_at>=c.created_at AND c.updated_at<=auth.checked_at
 AND isfinite(om.created_at) AND isfinite(om.updated_at) AND om.updated_at>=om.created_at AND om.updated_at<=auth.checked_at
 AND (c.city_id IS NULL OR (city.publication_status='published' AND cc.status='active'
 AND isfinite(city.updated_at) AND city.updated_at<=auth.checked_at
 AND isfinite(cc.updated_at) AND cc.updated_at<=auth.checked_at
 AND (city.expires_at IS NULL OR (isfinite(city.expires_at) AND city.expires_at>auth.checked_at))))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=auth.owner_id AND b.blocked_account_id IN(co.id,creator.id))
 OR (b.blocker_account_id IN(co.id,creator.id) AND b.blocked_account_id=auth.owner_id))` + memberTimes, nil
	case Organization:
		return nativeAuthoritySQL + nativeColumns + `
 'resourceId',o.id,'principalAccountId',o.account_id,'label',o.name,'resourceCreatedAt',o.created_at,
 'resourceUpdatedAt',o.updated_at,'resourceRowToken',o.xmin::text,'principalRowToken',oa.xmin::text,
 'visibility',o.visibility,'organizationType',o.organization_type,
 'owners',(SELECT jsonb_agg(jsonb_build_object('id',owners.id,'owner',owners.user_account_id,'role',owners.role,
 'updatedAt',owners.updated_at,'memberToken',owners.xmin::text,'accountToken',p.xmin::text) ORDER BY owners.id)
 FROM organization_memberships owners JOIN accounts p ON p.id=owners.user_account_id AND p.account_type='person' AND p.status='active'
 WHERE owners.organization_id=o.id AND owners.role='owner' AND owners.status='active'))::text,NULL::timestamptz
 FROM authority auth JOIN organization_memberships m ON m.id=$5 AND m.user_account_id=auth.owner_id AND m.status='active'
 JOIN organizations o ON o.id=m.organization_id AND o.status='active' AND o.visibility IN ('public','private')
 JOIN accounts oa ON oa.id=o.account_id AND oa.account_type='organization' AND oa.status='active'
 WHERE m.role IN ('owner','admin','moderator','member') AND o.id<>o.account_id
 AND isfinite(o.created_at) AND isfinite(o.updated_at) AND o.updated_at>=o.created_at AND o.updated_at<=auth.checked_at
 AND EXISTS(SELECT 1 FROM organization_memberships owners JOIN accounts p ON p.id=owners.user_account_id
 AND p.account_type='person' AND p.status='active' WHERE owners.organization_id=o.id AND owners.status='active' AND owners.role='owner')
 AND NOT EXISTS(SELECT 1 FROM organization_memberships owners WHERE owners.organization_id=o.id
 AND owners.status='active' AND owners.role='owner' AND (NOT isfinite(owners.created_at) OR NOT isfinite(owners.updated_at)
 OR owners.updated_at<owners.created_at OR owners.updated_at>auth.checked_at))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
 (b.blocker_account_id=auth.owner_id AND (b.blocked_account_id=o.account_id OR b.blocked_account_id IN
 (SELECT user_account_id FROM organization_memberships WHERE organization_id=o.id AND role='owner' AND status='active')))
 OR (b.blocked_account_id=auth.owner_id AND (b.blocker_account_id=o.account_id OR b.blocker_account_id IN
 (SELECT user_account_id FROM organization_memberships WHERE organization_id=o.id AND role='owner' AND status='active'))))` + memberTimes, nil
	default:
		return "", ErrInvalid
	}
}

type nativePayload struct {
	MembershipID       string    `json:"membershipId"`
	OwnerID            string    `json:"ownerId"`
	Role               string    `json:"role"`
	Status             string    `json:"status"`
	ResourceID         string    `json:"resourceId"`
	PrincipalAccountID string    `json:"principalAccountId"`
	Label              string    `json:"label"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	ResourceCreatedAt  time.Time `json:"resourceCreatedAt"`
	ResourceUpdatedAt  time.Time `json:"resourceUpdatedAt"`
}
type nativeState struct {
	now           time.Time
	sessionLimit  time.Time
	sourceExpires *time.Time
	authority     string
	fact          Evidence
}

// clock is the same native database clock that issues source observations and
// checks session/resource validity. It is private, with no injection API.
func (n *nativeResolver) clock(ctx context.Context) (time.Time, error) {
	if n == nil || n.pool == nil {
		return time.Time{}, ErrUnavailable
	}
	var now time.Time
	if e := n.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil || !validTime(now) {
		return time.Time{}, ErrUnavailable
	}
	return now.UTC(), nil
}

func (n *nativeResolver) authenticate(ctx context.Context, r Request) error {
	if n == nil || n.pool == nil || n.store == nil {
		return ErrUnavailable
	}
	actor, e := n.store.Authenticate(ctx, r.Access.SessionDigest)
	if errors.Is(e, identity.ErrUnauthorized) {
		return ErrDenied
	}
	if e != nil {
		return ErrUnavailable
	}
	if actor.AccountType != "person" || actor.ID != r.Access.WorkspacePrincipal.ID {
		return ErrDenied
	}
	return nil
}
func (n *nativeResolver) load(ctx context.Context, r Request) (nativeState, error) {
	var s nativeState
	if n == nil || n.pool == nil {
		return s, ErrUnavailable
	}
	sql, e := nativeSQL(r.Kind)
	if e != nil {
		return s, e
	}
	tx, e := n.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if e != nil {
		return s, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return s, ErrUnavailable
	}
	var raw string
	e = tx.QueryRow(ctx, sql, r.Access.SessionDigest[:], r.Access.WorkspacePrincipal.ID, r.Agent.AgentID, n.development, r.MembershipID).Scan(&s.now, &s.sessionLimit, &s.authority, &raw, &s.sourceExpires)
	if errors.Is(e, pgx.ErrNoRows) {
		return nativeState{}, ErrDenied
	}
	if e != nil {
		return nativeState{}, ErrUnavailable
	}
	var p nativePayload
	if len(raw) > 64*1024 || json.Unmarshal([]byte(raw), &p) != nil || !validTime(s.now) || !validTime(s.sessionLimit) || !s.sessionLimit.After(s.now) {
		return nativeState{}, ErrUnavailable
	}
	for _, t := range []time.Time{p.CreatedAt, p.UpdatedAt, p.ResourceCreatedAt, p.ResourceUpdatedAt} {
		if !validTime(t) || t.After(s.now) {
			return nativeState{}, ErrDenied
		}
	}
	if p.MembershipID != r.MembershipID || p.OwnerID != r.Access.WorkspacePrincipal.ID || p.UpdatedAt.Before(p.CreatedAt) || p.ResourceUpdatedAt.Before(p.ResourceCreatedAt) {
		return nativeState{}, ErrDenied
	}
	if s.sourceExpires != nil && (!validTime(*s.sourceExpires) || !s.sourceExpires.After(s.now)) {
		return nativeState{}, ErrDenied
	}
	version, e := membershipVersion(r.Kind, p.UpdatedAt, []byte(raw))
	if e != nil {
		return nativeState{}, ErrDenied
	}
	s.fact = Evidence{Kind: r.Kind, MembershipID: p.MembershipID, ResourceID: p.ResourceID, PrincipalAccountID: p.PrincipalAccountID, Label: p.Label, Role: p.Role, Status: p.Status, SourceVersion: version, NativeTime: p.UpdatedAt.UTC()}
	if e = tx.Commit(ctx); e != nil {
		return nativeState{}, ErrUnavailable
	}
	return s, nil
}
