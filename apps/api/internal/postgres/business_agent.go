package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ agentbusiness.Store = (*Store)(nil)

const businessKnowledgeTables = `LOCK TABLE businesses,business_memberships,business_console_profiles,business_console_venue_facts,business_venue_relations,accounts,sessions,venues,venue_candidates,places,cities,organizations IN ACCESS SHARE MODE`

// One payload statement is the source ACL linearization point. The stamp is
// excluded from the opaque content/binding token, not from temporal checks.
const businessKnowledgeSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at),
 authority AS (SELECT b.id,b.account_id,b.xmin::text AS business_xmin,a.xmin::text AS actor_xmin,
 principal.xmin::text AS principal_xmin,m.xmin::text AS member_xmin,b.claim_reviewed_at,
 EXISTS(SELECT 1 FROM sessions se WHERE se.token_sha256=$3 AND se.account_id=a.id AND se.revoked_at IS NULL
 AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ($4::boolean OR se.authentication_method<>'dev_phone')) AS session_valid
 FROM businesses b JOIN accounts principal ON principal.id=b.account_id AND principal.account_type='business' AND principal.status='active'
 JOIN accounts a ON a.id=$2 AND a.account_type='person' AND a.status='active'
 JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=a.id AND m.status='active' AND m.role IN ('owner','admin'),stamp
 WHERE b.id=$1 AND b.status='active' AND b.claim_status='verified' AND b.claim_source_url IS NOT NULL
 AND b.claim_reviewed_by IS NOT NULL AND isfinite(b.claim_reviewed_at) AND b.claim_reviewed_at<=stamp.at),
 profile AS (SELECT jsonb_build_object('version',f.version,'validUntil',f.valid_until,'facts',f.facts) AS payload,
 f.xmin::text AS source_xmin,f.valid_until AS deadline FROM business_console_profiles f,stamp WHERE f.business_id=$1 AND f.state='verified'
 AND isfinite(f.valid_until) AND f.valid_until>stamp.at AND isfinite(f.reviewed_at) AND f.reviewed_at<=stamp.at),
 venue AS (SELECT f.place_id,jsonb_build_object('placeId',f.place_id,'version',f.version,'validUntil',f.valid_until,'facts',f.facts) AS payload,
 jsonb_build_array(f.xmin::text,r.xmin::text,v.xmin::text,vc.xmin::text,p.xmin::text,c.xmin::text,o.xmin::text) AS source_xmins,
 least(f.valid_until,v.expires_at,p.expires_at,c.expires_at) AS deadline
 FROM business_console_venue_facts f JOIN business_venue_relations r ON r.business_id=f.business_id AND r.place_id=f.place_id AND r.status='verified'
 JOIN venues v ON v.place_id=f.place_id JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved'
 AND vc.place_id=v.place_id AND vc.city_id=v.city_id AND vc.reviewed_by=v.reviewed_by
 JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id AND p.publication_status='published'
 JOIN cities c ON c.id=p.city_id AND c.publication_status='published'
 LEFT JOIN organizations o ON o.id=v.operator_organization_id,stamp
 WHERE f.business_id=$1 AND f.state='verified' AND isfinite(f.valid_until) AND f.valid_until>stamp.at
 AND isfinite(f.reviewed_at) AND f.reviewed_at<=stamp.at AND isfinite(r.reviewed_at) AND r.reviewed_at<=stamp.at
 AND isfinite(v.expires_at) AND v.expires_at>stamp.at
 AND (v.operator_organization_id IS NULL OR o.status='active')
 AND (p.expires_at IS NULL OR (isfinite(p.expires_at) AND p.expires_at>stamp.at))
 AND (c.expires_at IS NULL OR (isfinite(c.expires_at) AND c.expires_at>stamp.at)) ORDER BY f.place_id LIMIT 101)
 SELECT stamp.at,EXISTS(SELECT 1 FROM sessions se JOIN accounts actor ON actor.id=se.account_id
 WHERE se.token_sha256=$3 AND actor.id=$2 AND actor.account_type='person' AND actor.status='active'
 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at
 AND ($4::boolean OR se.authentication_method<>'dev_phone')),EXISTS(SELECT 1 FROM authority),
 jsonb_build_object('authority',(SELECT to_jsonb(a)-'session_valid' FROM authority a),
 'profile',(SELECT payload FROM profile),'profileXmin',(SELECT source_xmin FROM profile),
 'venues',coalesce((SELECT jsonb_agg(payload ORDER BY place_id) FROM venue),'[]'::jsonb),
 'venueXmins',coalesce((SELECT jsonb_agg(source_xmins ORDER BY place_id) FROM venue),'[]'::jsonb)),
 (SELECT min(deadline) FROM (SELECT deadline FROM profile UNION ALL SELECT deadline FROM venue) deadlines) FROM stamp`

func (s *Store) ReadOwnBusinessKnowledge(ctx context.Context, a businessconsole.Access) (agentbusiness.Snapshot, error) {
	var out agentbusiness.Snapshot
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return out, agentbusiness.ErrUnavailable
	}
	if businessconsole.ValidateAccess(a, true) != nil {
		return out, businessconsole.ErrForbidden
	}
	// PostgreSQL row SHARE locks require a read-write transaction. The domain
	// implementation contains no INSERT/UPDATE/DELETE; this is not SQL READ ONLY.
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return out, agentbusiness.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, agentbusiness.ErrUnavailable
	}
	// Table/source waits precede the Session lock. Business SHARE is compatible
	// with existing activity/outbox readers and serializes Console mutation.
	if _, e = tx.Exec(ctx, businessKnowledgeTables); e != nil {
		return out, agentbusiness.ErrUnavailable
	}
	var principal string
	e = tx.QueryRow(ctx, `SELECT account_id FROM businesses WHERE id=$1 AND status='active' FOR SHARE`, a.BusinessID).Scan(&principal)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, businessconsole.ErrNotFound
	}
	if e != nil {
		return out, agentbusiness.ErrUnavailable
	}
	rows, e := tx.Query(ctx, `SELECT id FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, []string{a.ActingPersonID, principal})
	if e != nil {
		return out, agentbusiness.ErrUnavailable
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, agentbusiness.ErrUnavailable
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return out, agentbusiness.ErrUnavailable
	}
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM business_memberships WHERE business_id=$1 AND user_account_id=$2 AND status='active' AND role IN ('owner','admin') FOR SHARE`, a.BusinessID, a.ActingPersonID).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, businessconsole.ErrForbidden
	}
	if e != nil {
		return out, agentbusiness.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID); e != nil {
		return out, e
	}
	// Session failure is 401 even when source has concurrently become invalid.
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID, s.devPhoneEnabled); e != nil {
		return out, e
	}
	var raw []byte
	var current, session bool
	var deadline *time.Time
	e = tx.QueryRow(ctx, businessKnowledgeSQL, a.BusinessID, a.ActingPersonID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&out.At, &session, &current, &raw, &deadline)
	if e != nil {
		return agentbusiness.Snapshot{}, fmt.Errorf("%w: native knowledge payload: %v", agentbusiness.ErrUnavailable, e)
	}
	if ctx.Err() != nil {
		return agentbusiness.Snapshot{}, agentbusiness.ErrUnavailable
	}
	if !session {
		return agentbusiness.Snapshot{}, identity.ErrUnauthorized
	}
	if !current {
		return agentbusiness.Snapshot{}, businessconsole.ErrForbidden
	}
	var payload struct {
		Profile *agentbusiness.Profile `json:"profile"`
		Venues  []agentbusiness.Venue  `json:"venues"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return agentbusiness.Snapshot{}, agentbusiness.ErrUnavailable
	}
	out.Actor = actorref.ActorRef{Type: actorref.Business, ID: a.BusinessID}
	out.Principal = actorref.PrincipalRef{Type: actorref.Business, ID: principal}
	out.Profile = payload.Profile
	out.Venues = payload.Venues
	out.At = out.At.UTC()
	// RFC3339 UTC decoding may reuse time.Local when the process itself is UTC.
	// Normalize explicit native timestamps, independent of that pointer identity.
	if out.Profile != nil {
		out.Profile.ValidUntil = out.Profile.ValidUntil.UTC()
	}
	for i := range out.Venues {
		out.Venues[i].ValidUntil = out.Venues[i].ValidUntil.UTC()
	}
	out.SourceVersion = agentbusiness.Digest(raw)
	if e = agentbusiness.ValidateSnapshot(out); e != nil {
		return agentbusiness.Snapshot{}, fmt.Errorf("%w: %v", agentbusiness.ErrUnavailable, e)
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.ActingPersonID, s.devPhoneEnabled); e != nil {
		return agentbusiness.Snapshot{}, e
	}
	var finalTime time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&finalTime); e != nil || finalTime.Before(out.At) {
		return agentbusiness.Snapshot{}, agentbusiness.ErrUnavailable
	}
	if deadline != nil && !deadline.After(finalTime) {
		return agentbusiness.Snapshot{}, agentbusiness.ErrChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return agentbusiness.Snapshot{}, agentbusiness.ErrUnavailable
	}
	return out, nil
}

// Re-materialize the same restricted current source after JSON encoding.
// This is not a permanent grant or a promise about changes after the statement.
func (s *Store) ValidateOwnBusinessKnowledge(ctx context.Context, a businessconsole.Access, v string) error {
	if !agentbusiness.ValidVersion(v) {
		return agentbusiness.ErrInvalid
	}
	current, e := s.ReadOwnBusinessKnowledge(ctx, a)
	if e != nil {
		return e
	}
	if current.SourceVersion != v {
		return agentbusiness.ErrChanged
	}
	// Read above held Account then Session, checked fresh source/session/expiry,
	// and never refreshed idle. Do not introduce a second unrelated lock wait.
	return nil
}
