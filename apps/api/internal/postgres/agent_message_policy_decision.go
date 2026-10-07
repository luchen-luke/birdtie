package postgres

import (
	"context"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

type messageRouteFence struct {
	actor, peer, static, version string
	route                        mp.Disposition
	until, checkedAt             time.Time
	configured, blocked          bool
}

// This transaction uses current original domain rows; it creates no grant.
// Pair accounts are acquired in UUID order before absent-relation table locks.
func startMessageRoute(ctx context.Context, tx pgx.Tx, actor, peer string) (messageRouteFence, error) {
	f := messageRouteFence{actor: actor, peer: peer}
	if e := messageAuthenticateCurrent(ctx, tx, actor); e != nil {
		return f, e
	}
	if actor == peer {
		return f, connection.ErrNotFound
	}
	if _, e := tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return f, mp.ErrUnavailable
	}
	rows, e := tx.Query(ctx, `SELECT id FROM accounts WHERE id=ANY($1::uuid[]) AND account_type='person' AND status='active' ORDER BY id FOR NO KEY UPDATE`, []string{actor, peer})
	if e != nil {
		return f, mp.ErrUnavailable
	}
	n := 0
	for rows.Next() {
		n++
	}
	rows.Close()
	if rows.Err() != nil {
		return f, mp.ErrUnavailable
	}
	if e = messageAuthenticateCurrent(ctx, tx, actor); e != nil {
		return f, e
	}
	if n != 2 {
		return f, connection.ErrNotFound
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE account_blocks,user_profiles,intents,cities,agents,agent_profiles,agent_message_request_policies IN SHARE MODE;LOCK TABLE connection_requests,person_ties,conversations IN SHARE ROW EXCLUSIVE MODE`); e != nil {
		return f, mp.ErrUnavailable
	}
	if e = messageLockCurrent(ctx, tx, actor); e != nil {
		return f, e
	}
	if e = tx.QueryRow(ctx, messagePolicyStaticSQL, actor, peer).Scan(&f.static); e != nil {
		return f, mp.ErrUnavailable
	}
	var accepted, public, binding bool
	var mode *string
	var expires, from *time.Time
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() AS checked_at) SELECT n.checked_at,
 EXISTS(SELECT 1 FROM account_blocks WHERE (blocker_account_id=$1 AND blocked_account_id=$2) OR (blocker_account_id=$2 AND blocked_account_id=$1)),
 EXISTS(SELECT 1 FROM person_ties t JOIN connection_requests r ON r.id=t.request_id AND r.state='accepted' AND r.scope='friend' AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id WHERE t.status='active' AND t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid)),
 EXISTS(SELECT 1 FROM user_profiles WHERE account_id=$2 AND visibility='public'),p.incoming_requests,p.valid_from,p.expires_at,
 p.agent_id IS NULL OR EXISTS(SELECT 1 FROM agents ag JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=ag.principal_account_id AND ap.owner_type='PERSON' WHERE ag.id=p.agent_id AND ag.principal_account_id=$2 AND ag.agent_type='personal' AND ag.status='active')
 FROM n LEFT JOIN agent_message_request_policies p ON p.owner_id=$2`, actor, peer).Scan(&f.checkedAt, &f.blocked, &accepted, &public, &mode, &from, &expires, &binding)
	if e != nil {
		return f, mp.ErrUnavailable
	}
	facts := mp.RoutingFacts{Blocked: f.blocked, AcceptedTie: accepted, PublicPerson: public, Configured: mode != nil, CurrentBinding: binding}
	if mode != nil {
		facts.Incoming = mp.Disposition(*mode)
	}
	if from != nil {
		facts.ValidFrom = *from
	}
	if expires != nil {
		facts.ExpiresAt = *expires
	}
	f.configured = facts.Configured
	f.route = mp.Route(facts, f.checkedAt)
	f.until = f.checkedAt.Add(30 * time.Second)
	if f.route != mp.Allow && expires != nil && expires.After(f.checkedAt) && expires.Before(f.until) {
		f.until = *expires
	}
	e = tx.QueryRow(ctx, `SELECT encode(sha256(convert_to(jsonb_build_array($3::text,COALESCE((SELECT jsonb_agg(jsonb_build_object('id',t.id,'status',t.status,'request',t.request_id,'xmin',t.xmin::text,'rxmin',r.xmin::text) ORDER BY t.id) FROM person_ties t JOIN connection_requests r ON r.id=t.request_id WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid)),'[]'::jsonb))::text,'UTF8')),'hex')`, actor, peer, f.static).Scan(&f.version)
	if e != nil {
		return f, mp.ErrUnavailable
	}
	if a := messageCurrentContext(ctx); a != nil && a.version != "" && a.version != f.version {
		return f, mp.ErrChanged
	}
	return f, nil
}

const messagePolicyStaticSQL = `SELECT encode(sha256(convert_to(jsonb_build_array(
 (SELECT jsonb_build_object('id',id,'type',account_type,'status',status,'xmin',xmin::text) FROM accounts WHERE id=$1),
 (SELECT jsonb_build_object('id',id,'type',account_type,'status',status,'xmin',xmin::text) FROM accounts WHERE id=$2),
 (SELECT jsonb_build_object('id',account_id,'visibility',visibility,'xmin',xmin::text) FROM user_profiles WHERE account_id=$2),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('blocker',blocker_account_id,'blocked',blocked_account_id,'created',created_at,'xmin',xmin::text) ORDER BY blocker_account_id,blocked_account_id) FROM account_blocks WHERE (blocker_account_id=$1 AND blocked_account_id=$2) OR (blocker_account_id=$2 AND blocked_account_id=$1)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('xmin',p.xmin::text) ORDER BY p.agent_id) FROM agent_message_request_policies p WHERE p.owner_id=$2),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',ag.id,'status',ag.status,'xmin',ag.xmin::text,'apxmin',ap.xmin::text) ORDER BY ag.id) FROM agents ag LEFT JOIN agent_profiles ap ON ap.agent_id=ag.id WHERE ag.principal_account_id=$2 AND ag.agent_type='personal'),'[]'::jsonb))::text,'UTF8')),'hex')`

func finishMessageRoute(ctx context.Context, tx pgx.Tx, f messageRouteFence) error {
	return finishMessageRouteForActor(ctx, tx, f, f.actor)
}
func finishMessageRouteForActor(ctx context.Context, tx pgx.Tx, f messageRouteFence, actor string) error {
	var token string
	var now time.Time
	var current bool
	var digest [32]byte
	var dev bool
	a := messageCurrentContext(ctx)
	if a != nil {
		if a.access.Actor.ID != actor || !a.access.Valid() || a.access.Actor.AccountType != "person" {
			return identity.ErrUnauthorized
		}
		digest = a.access.SessionDigest
		dev = a.dev
	}
	if tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() AS checked_at) SELECT n.checked_at,(`+messagePolicyStaticSQL+`),($3::boolean OR EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id WHERE s.token_sha256=$4 AND a.id=$6 AND a.account_type='person' AND a.status='active' AND s.revoked_at IS NULL AND s.expires_at>n.checked_at AND s.idle_expires_at>n.checked_at AND ($5::boolean OR s.authentication_method<>'dev_phone'))) FROM n`, f.actor, f.peer, a == nil, digest[:], dev, actor).Scan(&now, &token, &current) != nil {
		return mp.ErrUnavailable
	}
	if !current {
		return identity.ErrUnauthorized
	}
	if !now.Before(f.until) || token != f.static {
		return mp.ErrChanged
	}
	return nil
}
func (s *Store) ReadMessagePolicyDecision(ctx context.Context, a agentprofile.PrivateAccess, peer string) (mp.Decision, error) {
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return mp.Decision{}, mp.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return mp.Decision{}, mp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	ctx, e = s.messageCurrentAccess(ctx, eaMessageAccess(a), "")
	if e != nil {
		return mp.Decision{}, e
	}
	f, e := startMessageRoute(ctx, tx, a.WorkspacePrincipal.ID, peer)
	if errors.Is(e, connection.ErrNotFound) {
		var now time.Time
		if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
			return mp.Decision{}, mp.ErrUnavailable
		}
		if e = messageAuthenticateCurrent(ctx, tx, a.WorkspacePrincipal.ID); e != nil {
			return mp.Decision{}, e
		}
		return mp.Decision{Schema: mp.Schema, Disposition: mp.Block, Authority: mp.Authority, ObservedAt: now.UTC(), ValidUntil: now.UTC()}, nil
	}
	if e != nil {
		return mp.Decision{}, e
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return mp.Decision{}, e
	}
	d := mp.Decision{Schema: mp.Schema, Disposition: f.route, SourceVersion: f.version, ObservedAt: f.checkedAt.UTC(), ValidUntil: f.until.UTC(), Authority: mp.Authority}
	if f.route == mp.Block {
		d.SourceVersion = ""
	}
	if tx.Commit(ctx) != nil {
		return mp.Decision{}, mp.ErrUnavailable
	}
	return d, nil
}
func eaMessageAccess(a agentprofile.PrivateAccess) ea.Access {
	return ea.Access{Actor: identity.Actor{ID: a.WorkspacePrincipal.ID, AccountType: "person"}, SessionDigest: a.SessionDigest}
}
