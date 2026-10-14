package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

var _ connection.CloseCurrentStore = (*Store)(nil)
var _ connection.CloseResponseValidator = (*Store)(nil)

type closeSessionBinding struct {
	session       string
	generation    int64
	expires, idle time.Time
}
type closeResponseProof struct {
	store                             *Store
	owner, peer, session, kind, epoch string
	generation                        int64
	digest                            [32]byte
	wire                              [32]byte
	until                             time.Time
}

func closeHash(v any) [32]byte { b, _ := json.Marshal(v); return sha256.Sum256(b) }
func closeEntriesEpoch(v []connection.CloseEntry, truncated bool) string {
	h := closeHash(struct {
		Entries   []connection.CloseEntry
		Truncated bool
	}{v, truncated})
	return hex.EncodeToString(h[:])
}
func closeSourceVersion(epoch string, b closeSessionBinding, until time.Time) string {
	h := sha256.Sum256([]byte("birdtie-close-source-v1\n" + epoch + "\n" + b.session + "\n" + strconv.FormatInt(b.generation, 10) + "\n" + until.UTC().Format("2006-01-02T15:04:05.000000Z")))
	return hex.EncodeToString(h[:])
}

// Native sources include the current name projection and all facts on which
// its human ACL depends. No raw fields, token or hash preimage leaves the Store.
const closeEpochSQL = `SELECT encode(sha256(convert_to(jsonb_build_array(
 (SELECT to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) FROM accounts a WHERE a.id=$1),
 (SELECT to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) FROM accounts a WHERE a.id=$2),
 COALESCE((SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('xmin',p.xmin::text) ORDER BY p.account_id) FROM user_profiles p WHERE p.account_id IN($1,$2)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(ag)||jsonb_build_object('xmin',ag.xmin::text,'profile',to_jsonb(ap),'profileXmin',ap.xmin::text) ORDER BY ag.id) FROM agents ag LEFT JOIN agent_profiles ap ON ap.agent_id=ag.id WHERE ag.principal_account_id IN($1,$2) AND ag.agent_type='personal'),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(v)||jsonb_build_object('xmin',v.xmin::text) ORDER BY v.owner_id) FROM agent_profile_field_visibility v WHERE v.owner_id IN($1,$2)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(t)||jsonb_build_object('xmin',t.xmin::text,'request',to_jsonb(r),'requestXmin',r.xmin::text) ORDER BY t.id) FROM person_ties t JOIN connection_requests r ON r.id=t.request_id WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(c)||jsonb_build_object('xmin',c.xmin::text) ORDER BY c.owner_account_id,c.person_account_id) FROM person_close_connections c WHERE ((c.owner_account_id=$1 AND c.person_account_id=$2) OR (c.owner_account_id=$2 AND c.person_account_id=$1)) AND (NOT $5::boolean OR c.owner_account_id<>$1)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY x.blocker_account_id,x.blocked_account_id) FROM account_blocks x WHERE (x.blocker_account_id=$1 AND x.blocked_account_id=$2) OR (x.blocker_account_id=$2 AND x.blocked_account_id=$1)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(f)||jsonb_build_object('xmin',f.xmin::text) ORDER BY f.id) FROM follows f WHERE (f.follower_account_id=$1 AND f.person_account_id=$2) OR (f.follower_account_id=$2 AND f.person_account_id=$1)),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(g)||jsonb_build_object('xmin',g.xmin::text) ORDER BY g.id) FROM consent_grants g WHERE g.resource_type='profile' AND g.purpose='profile_view' AND ((g.owner_account_id=$1 AND g.recipient_account_id=$2) OR (g.owner_account_id=$2 AND g.recipient_account_id=$1))),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(m)||jsonb_build_object('xmin',m.xmin::text,'community',to_jsonb(c),'communityXmin',c.xmin::text) ORDER BY m.community_id,m.user_account_id) FROM community_memberships m JOIN communities c ON c.id=m.community_id WHERE m.user_account_id IN($1,$2)),'[]'::jsonb),$3::text,$4::boolean)::text,'UTF8')),'hex')`

func (s *Store) beginClose(ctx context.Context, a ea.Access, peer string, write bool) (pgx.Tx, closeSessionBinding, error) {
	var b closeSessionBinding
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, b, connection.ErrCloseUnavailable
	}
	if !connection.ValidCloseAccess(a) {
		return nil, b, identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, connection.ErrCloseUnavailable
	}
	fail := func(e error) (pgx.Tx, closeSessionBinding, error) {
		tx.Rollback(context.Background())
		return nil, b, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(connection.ErrCloseUnavailable)
	}
	// Match native human writers: sorted Accounts, native relations, Session.
	peers := []string{a.Actor.ID}
	if peer != "" {
		peers = append(peers, peer)
	}
	mode := "SHARE"
	if write {
		mode = "NO KEY UPDATE"
	}
	rows, e := tx.Query(ctx, `SELECT id FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR `+mode, peers)
	if e != nil {
		return fail(connection.ErrCloseUnavailable)
	}
	own := false
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return fail(connection.ErrCloseUnavailable)
		}
		own = own || id == a.Actor.ID
	}
	rows.Close()
	if rows.Err() != nil {
		return fail(connection.ErrCloseUnavailable)
	}
	if !own {
		return fail(identity.ErrUnauthorized)
	}
	// Table locks protect absent Block/Close rows and preserve old writer order.
	if _, e = tx.Exec(ctx, `LOCK TABLE account_blocks IN SHARE MODE; LOCK TABLE connection_requests,person_ties,person_close_connections IN SHARE ROW EXCLUSIVE MODE; LOCK TABLE user_profiles,agents,agent_profiles,agent_profile_field_visibility,follows,consent_grants,communities,community_memberships IN ACCESS SHARE MODE`); e != nil {
		return fail(connection.ErrCloseUnavailable)
	}
	e = tx.QueryRow(ctx, `SELECT se.id,se.authorization_generation,se.expires_at,se.idle_expires_at FROM sessions se JOIN accounts a ON a.id=se.account_id WHERE se.token_sha256=$1 AND a.id=$2 AND a.account_type='person' AND a.status='active' AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp() AND ($3::boolean OR se.authentication_method<>'dev_phone') FOR SHARE OF se`, a.SessionDigest[:], a.Actor.ID, s.devPhoneEnabled).Scan(&b.session, &b.generation, &b.expires, &b.idle)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(identity.ErrUnauthorized)
	}
	if e != nil {
		return fail(connection.ErrCloseUnavailable)
	}
	return tx, b, nil
}
func (s *Store) closeFinalClock(ctx context.Context, tx pgx.Tx, a ea.Access, b closeSessionBinding) (time.Time, error) {
	var n time.Time
	var current bool
	e := tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() AS now) SELECT n.now,EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id WHERE se.id=$1 AND se.token_sha256=$2 AND a.id=$3 AND a.account_type='person' AND a.status='active' AND se.authorization_generation=$4 AND se.revoked_at IS NULL AND se.expires_at>n.now AND se.idle_expires_at>n.now AND ($5::boolean OR se.authentication_method<>'dev_phone')) FROM n`, b.session, a.SessionDigest[:], a.Actor.ID, b.generation, s.devPhoneEnabled).Scan(&n, &current)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, connection.ErrCloseUnavailable
	}
	if !current {
		return time.Time{}, identity.ErrUnauthorized
	}
	return n.UTC(), nil
}
func closeDeadline(n time.Time, b closeSessionBinding) time.Time {
	u := n.Add(30 * time.Second)
	if b.expires.Before(u) {
		u = b.expires
	}
	if b.idle.Before(u) {
		u = b.idle
	}
	return u.UTC()
}
func (s *Store) closeNameDeadline(ctx context.Context, tx pgx.Tx, owner, peer string, n time.Time, b closeSessionBinding) (time.Time, error) {
	var target any
	if peer != "" {
		target = peer
	}
	var until time.Time
	// Expiration changes no row/xmin. Bind the original name response to the
	// earliest relevant grant/community clock boundary, as well as Session.
	// This cap is conservative; it does not grant a name, Close or model access.
	e := tx.QueryRow(ctx, `WITH peers AS MATERIALIZED(
	 SELECT CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END id
	 FROM person_ties t WHERE (t.person_a_account_id=$1 OR t.person_b_account_id=$1)
	 AND ($2::uuid IS NULL OR $2 IN(t.person_a_account_id,t.person_b_account_id))
	 AND birdtie_connection_visible($1,CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END))
	 SELECT LEAST($3::timestamptz,
	 (SELECT min(g.expires_at) FROM consent_grants g JOIN peers p ON p.id=g.owner_account_id
	 WHERE g.recipient_account_id=$1 AND g.resource_type='profile' AND g.resource_id=g.owner_account_id::text
	 AND g.purpose='profile_view' AND g.actions @> ARRAY['read']::text[] AND g.revoked_at IS NULL AND g.expires_at>$4),
	 (SELECT min(c.expires_at) FROM communities c JOIN community_memberships own ON own.community_id=c.id AND own.user_account_id=$1 AND own.status='active'
	 JOIN community_memberships other ON other.community_id=c.id AND other.status='active' JOIN peers p ON p.id=other.user_account_id
	 WHERE c.lifecycle_status='active' AND c.publication_status='published' AND c.expires_at>$4))`, owner, target, closeDeadline(n, b), n).Scan(&until)
	if e != nil {
		return time.Time{}, connection.ErrCloseUnavailable
	}
	return until.UTC(), nil
}
func (s *Store) closeNativeEpoch(ctx context.Context, tx pgx.Tx, owner, peer, name string, excludeOwn bool) (string, error) {
	var full bool
	if tx.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_profile_visible_to(uuid,uuid)') IS NOT NULL`).Scan(&full) != nil {
		return "", connection.ErrCloseUnavailable
	}
	var epoch string
	if tx.QueryRow(ctx, closeEpochSQL, owner, peer, name, full, excludeOwn).Scan(&epoch) != nil {
		return "", connection.ErrCloseUnavailable
	}
	return epoch, nil
}
func (s *Store) closeEntry(ctx context.Context, tx pgx.Tx, a ea.Access, b closeSessionBinding, peer string, until time.Time) (connection.CloseEntry, error) {
	v := connection.CloseEntry{PersonID: peer, DisplayName: "Birdtie 成员"}
	e := tx.QueryRow(ctx, `SELECT t.id,t.request_id,COALESCE(c.is_active,false),COALESCE(c.native_revision,0) FROM person_ties t LEFT JOIN person_close_connections c ON c.owner_account_id=$1 AND c.person_account_id=$2 AND c.tie_id=t.id AND c.request_id=t.request_id WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid) AND birdtie_connection_visible($1,$2)`, a.Actor.ID, peer).Scan(&v.TieID, &v.RequestID, &v.IsClose, &v.NativeRevision)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, connection.ErrNotFound
	}
	if e != nil {
		return v, connection.ErrCloseUnavailable
	}
	// Tombstone revision must participate even if its retired source predates the
	// current reaccepted Friend. It can never be silently reset to zero.
	if tx.QueryRow(ctx, `SELECT COALESCE((SELECT native_revision FROM person_close_connections WHERE owner_account_id=$1 AND person_account_id=$2),0)`, a.Actor.ID, peer).Scan(&v.NativeRevision) != nil {
		return v, connection.ErrCloseUnavailable
	}
	var full bool
	if tx.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_profile_visible_to(uuid,uuid)') IS NOT NULL`).Scan(&full) != nil {
		return v, connection.ErrCloseUnavailable
	}
	if full {
		e = tx.QueryRow(ctx, `SELECT CASE WHEN birdtie_profile_visible_to($2,$1) AND birdtie_agent_profile_field_allowed($2,$1,'displayName') THEN COALESCE(NULLIF(p.display_name,''),'Birdtie 成员') ELSE 'Birdtie 成员' END FROM user_profiles p WHERE p.account_id=$2`, a.Actor.ID, peer).Scan(&v.DisplayName)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return v, connection.ErrCloseUnavailable
		}
	}
	epoch, e := s.closeNativeEpoch(ctx, tx, a.Actor.ID, peer, v.DisplayName, false)
	if e != nil {
		return v, e
	}
	v.SourceVersion = closeSourceVersion(epoch, b, until)
	if !connection.ValidCloseEntry(v, a.Actor.ID) {
		return v, connection.ErrCloseUnavailable
	}
	return v, nil
}
func (s *Store) closeVector(ctx context.Context, tx pgx.Tx, a ea.Access, b closeSessionBinding, until time.Time) ([]connection.CloseEntry, bool, error) {
	rows, e := tx.Query(ctx, `SELECT CASE WHEN person_a_account_id=$1 THEN person_b_account_id ELSE person_a_account_id END FROM person_ties WHERE status='active' AND (person_a_account_id=$1 OR person_b_account_id=$1) AND birdtie_connection_visible($1,CASE WHEN person_a_account_id=$1 THEN person_b_account_id ELSE person_a_account_id END) ORDER BY created_at DESC,id DESC LIMIT 101`, a.Actor.ID)
	if e != nil {
		return nil, false, connection.ErrCloseUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var p string
		if rows.Scan(&p) != nil {
			rows.Close()
			return nil, false, connection.ErrCloseUnavailable
		}
		ids = append(ids, p)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, false, connection.ErrCloseUnavailable
	}
	truncated := len(ids) > connection.CloseLimit
	if truncated {
		ids = ids[:connection.CloseLimit]
	}
	out := make([]connection.CloseEntry, 0, len(ids))
	for _, p := range ids {
		v, e := s.closeEntry(ctx, tx, a, b, p, until)
		if e != nil {
			return nil, false, e
		}
		out = append(out, v)
	}
	return out, truncated, nil
}
func (s *Store) closeSeal(a ea.Access, b closeSessionBinding, kind, peer, epoch string, until time.Time, value any) *closeResponseProof {
	return &closeResponseProof{store: s, owner: a.Actor.ID, peer: peer, session: b.session, generation: b.generation, digest: a.SessionDigest, kind: kind, epoch: epoch, until: until, wire: closeHash(value)}
}
func (s *Store) ListOwnCloseConnectionsCurrent(ctx context.Context, a ea.Access) (connection.CloseView, error) {
	v := connection.CloseView{SchemaVersion: connection.CloseSchema, OwnerID: a.Actor.ID, Connections: []connection.CloseEntry{}, Limit: connection.CloseLimit}
	tx, b, e := s.beginClose(ctx, a, "", false)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(context.Background())
	n, e := s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return v, e
	}
	v.ValidUntil, e = s.closeNameDeadline(ctx, tx, a.Actor.ID, "", n, b)
	if e != nil {
		return v, e
	}
	v.Connections, v.Truncated, e = s.closeVector(ctx, tx, a, b, v.ValidUntil)
	if e != nil {
		return v, e
	}
	v.ObservedAt, e = s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return v, e
	}
	if !connection.ValidCloseView(v, a.Actor.ID) {
		return v, connection.ErrCloseChanged
	}
	v.ResponseProof = s.closeSeal(a, b, "list", "", closeEntriesEpoch(v.Connections, v.Truncated), v.ValidUntil, v)
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return connection.CloseView{}, connection.ErrCloseUnavailable
	}
	return v, nil
}
func (s *Store) PutOwnCloseConnectionCurrent(ctx context.Context, a ea.Access, peer string, in connection.ClosePut) (connection.CloseResult, error) {
	var result connection.CloseResult
	if !connection.ValidCloseID(peer) || peer == a.Actor.ID || !connection.ValidClosePut(in) {
		return result, connection.ErrCloseInvalid
	}
	tx, b, e := s.beginClose(ctx, a, peer, true)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(context.Background())
	now, e := s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return result, e
	}
	currentDeadline, e := s.closeNameDeadline(ctx, tx, a.Actor.ID, peer, now, b)
	if e != nil {
		return result, e
	}
	if !now.Before(in.ValidUntil) || in.ValidUntil.After(currentDeadline) {
		return result, connection.ErrCloseChanged
	}
	before, e := s.closeEntry(ctx, tx, a, b, peer, in.ValidUntil)
	if e != nil {
		return result, e
	}
	if before.TieID != in.TieID || before.RequestID != in.RequestID || before.SourceVersion != in.SourceVersion {
		return result, connection.ErrCloseChanged
	}
	approvedEpoch, e := s.closeNativeEpoch(ctx, tx, a.Actor.ID, peer, before.DisplayName, true)
	if e != nil {
		return result, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO person_close_connections(owner_account_id,person_account_id,tie_id,request_id,is_active,selected_at) VALUES($1,$2,$3,$4,true,clock_timestamp()) ON CONFLICT(owner_account_id,person_account_id) DO UPDATE SET tie_id=EXCLUDED.tie_id,request_id=EXCLUDED.request_id,is_active=true,native_revision=person_close_connections.native_revision`, a.Actor.ID, peer, in.TieID, in.RequestID)
	if e != nil {
		return result, connection.ErrCloseUnavailable
	}
	if _, e = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose,target_resource_id) VALUES($1,'select','close_connection',$2,'allowed','human_close',$3)`, a.Actor.ID, peer, in.TieID); e != nil {
		return result, connection.ErrCloseUnavailable
	}
	result.Connection, e = s.closeEntry(ctx, tx, a, b, peer, in.ValidUntil)
	if e != nil {
		return result, e
	}
	currentApprovedEpoch, e := s.closeNativeEpoch(ctx, tx, a.Actor.ID, peer, result.Connection.DisplayName, true)
	if e != nil {
		return result, e
	}
	if currentApprovedEpoch != approvedEpoch {
		return result, connection.ErrCloseChanged
	}
	result.ObservedAt, e = s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return result, e
	}
	if !result.ObservedAt.Before(in.ValidUntil) {
		return result, connection.ErrCloseChanged
	}
	result.SchemaVersion = connection.CloseSchema
	result.OwnerID = a.Actor.ID
	result.ValidUntil = in.ValidUntil.UTC()
	if !connection.ValidCloseResult(result, a.Actor.ID, peer) {
		return result, connection.ErrCloseUnavailable
	}
	result.ResponseProof = s.closeSeal(a, b, "put", peer, result.Connection.SourceVersion, in.ValidUntil, result)
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return connection.CloseResult{}, connection.ErrCloseUnavailable
	}
	return result, nil
}
func (s *Store) DeleteOwnCloseConnectionCurrent(ctx context.Context, a ea.Access, peer string, in connection.CloseDelete) (connection.CloseDeleteReceipt, error) {
	result := connection.CloseDeleteReceipt{PersonID: peer}
	if !connection.ValidCloseID(peer) || peer == a.Actor.ID || !connection.ValidCloseDelete(in) {
		return result, connection.ErrCloseInvalid
	}
	tx, b, e := s.beginClose(ctx, a, peer, true)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(context.Background())
	var active bool
	var revision int64
	var tie, request string
	e = tx.QueryRow(ctx, `SELECT tie_id,request_id,is_active,native_revision FROM person_close_connections WHERE owner_account_id=$1 AND person_account_id=$2 FOR UPDATE`, a.Actor.ID, peer).Scan(&tie, &request, &active, &revision)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return result, connection.ErrCloseUnavailable
	}
	if e == nil {
		if tie != in.TieID || request != in.RequestID || revision < in.ExpectedRevision || active && revision != in.ExpectedRevision {
			return result, connection.ErrCloseChanged
		}
		if active {
			if _, e = tx.Exec(ctx, `UPDATE person_close_connections SET is_active=false WHERE owner_account_id=$1 AND person_account_id=$2`, a.Actor.ID, peer); e != nil {
				return result, connection.ErrCloseUnavailable
			}
			if _, e = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose,target_resource_id) VALUES($1,'remove','close_connection',$2,'allowed','human_close',$3)`, a.Actor.ID, peer, tie); e != nil {
				return result, connection.ErrCloseUnavailable
			}
		}
	}
	n, e := s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return result, e
	}
	until := closeDeadline(n, b)
	epoch, e := s.closeNativeEpoch(ctx, tx, a.Actor.ID, peer, "", false)
	if e != nil {
		return result, e
	}
	result.ResponseProof = s.closeSeal(a, b, "delete", peer, epoch, until, result)
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return connection.CloseDeleteReceipt{}, connection.ErrCloseUnavailable
	}
	return result, nil
}
func (s *Store) validateCloseResponse(ctx context.Context, a ea.Access, kind, peer string, value any, proof any) error {
	p, ok := proof.(*closeResponseProof)
	if !ok || p == nil || p.store != s || p.owner != a.Actor.ID || p.digest != a.SessionDigest || p.kind != kind || p.peer != peer || p.wire != closeHash(value) {
		return connection.ErrCloseUnavailable
	}
	tx, b, e := s.beginClose(ctx, a, peer, false)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	now, e := s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return e
	}
	if b.session != p.session || b.generation != p.generation || !now.Before(p.until) {
		return connection.ErrCloseChanged
	}
	var epoch string
	switch kind {
	case "list":
		v, tr, e := s.closeVector(ctx, tx, a, b, p.until)
		if e != nil {
			return e
		}
		epoch = closeEntriesEpoch(v, tr)
	case "put":
		v, e := s.closeEntry(ctx, tx, a, b, peer, p.until)
		if e != nil {
			return connection.ErrCloseChanged
		}
		epoch = v.SourceVersion
	case "delete":
		epoch, e = s.closeNativeEpoch(ctx, tx, a.Actor.ID, peer, "", false)
		if e != nil {
			return e
		}
		var active bool
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM person_close_connections WHERE owner_account_id=$1 AND person_account_id=$2 AND is_active)`, a.Actor.ID, peer).Scan(&active) != nil {
			return connection.ErrCloseUnavailable
		}
		if active {
			return connection.ErrCloseChanged
		}
	default:
		return connection.ErrCloseUnavailable
	}
	if epoch != p.epoch {
		return connection.ErrCloseChanged
	}
	now, e = s.closeFinalClock(ctx, tx, a, b)
	if e != nil {
		return e
	}
	if !now.Before(p.until) {
		return connection.ErrCloseChanged
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return connection.ErrCloseUnavailable
	}
	return nil
}
func (s *Store) ValidateCloseConnectionsResponse(ctx context.Context, a ea.Access, v connection.CloseView) error {
	if !connection.ValidCloseView(v, a.Actor.ID) {
		return connection.ErrCloseUnavailable
	}
	return s.validateCloseResponse(ctx, a, "list", "", v, v.ResponseProof)
}
func (s *Store) ValidateCloseConnectionResponse(ctx context.Context, a ea.Access, peer string, v connection.CloseResult) error {
	if !connection.ValidCloseResult(v, a.Actor.ID, peer) {
		return connection.ErrCloseUnavailable
	}
	return s.validateCloseResponse(ctx, a, "put", peer, v, v.ResponseProof)
}
func (s *Store) ValidateCloseDeleteResponse(ctx context.Context, a ea.Access, v connection.CloseDeleteReceipt) error {
	if !connection.ValidCloseID(v.PersonID) || v.PersonID == a.Actor.ID {
		return connection.ErrCloseUnavailable
	}
	return s.validateCloseResponse(ctx, a, "delete", v.PersonID, v, v.ResponseProof)
}
