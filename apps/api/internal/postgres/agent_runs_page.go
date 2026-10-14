package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

var runPageCodec = func() *ar.CursorCodec {
	k := make([]byte, 32)
	if _, e := rand.Read(k); e != nil {
		panic("run page cursor key unavailable")
	}
	c, e := ar.NewCursorCodec(k)
	if e != nil {
		panic("run page cursor unavailable")
	}
	return c
}()

type runPageProof struct {
	service *AgentRuns
	frame   ar.CursorFrame
	wire    [32]byte
}

func runPageWire(p ar.Page) [32]byte { p.Proof = nil; b, _ := json.Marshal(p); return sha256.Sum256(b) }

// Metadata polling does not renew last_seen or idle expiry.
func (s *AgentRuns) AuthenticateRunPage(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	var a identity.Actor
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil || digest == ([32]byte{}) {
		return a, identity.ErrUnauthorized
	}
	e := s.store.pool.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT a.id::text,a.account_type FROM sessions se JOIN accounts a ON a.id=se.account_id CROSS JOIN n WHERE se.token_sha256=$1 AND a.account_type='person' AND a.status='active' AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at AND ($2::boolean OR se.authentication_method<>'dev_phone')`, digest[:], s.store.devPhoneEnabled).Scan(&a.ID, &a.AccountType)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	if e != nil || ctx.Err() != nil {
		return identity.Actor{}, ar.ErrUnavailable
	}
	return a, nil
}

// Bind account/Agent/profile first; capture Session without locking it. The
// generation and clock are checked after every Run/table wait, Session last.
func (s *AgentRuns) runPageTx(ctx context.Context, a agentprofile.PrivateAccess, cursor string, proof *runPageProof) (ar.Page, error) {
	var out ar.Page
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil || agentprofile.ValidatePrivateAccess(a) != nil {
		return out, ar.ErrDenied
	}
	tx, e := s.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return out, ar.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, ar.ErrUnavailable
	}
	var originalSession string
	var originalGeneration int64
	e = tx.QueryRow(ctx, `SELECT id::text,authorization_generation FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp()`, a.SessionDigest[:], a.WorkspacePrincipal.ID).Scan(&originalSession, &originalGeneration)
	if e != nil {
		return out, ar.ErrDenied
	}
	if e = runGate(ctx, tx, false); e != nil {
		return out, e
	}
	b, e := memoryReviewBinding(ctx, tx, a, s.store.devPhoneEnabled)
	if e != nil {
		return out, runError(e)
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, false); e != nil {
		return out, runError(e)
	}
	var generation int64
	var now, expires time.Time
	var account, agent, profile string
	e = tx.QueryRow(ctx, `SELECT s.authorization_generation,clock_timestamp(),LEAST(s.expires_at,s.idle_expires_at),a.xmin::text,ag.xmin::text,ap.xmin::text FROM sessions s JOIN accounts a ON a.id=s.account_id JOIN agents ag ON ag.id=$2 AND ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE s.id=$1`, b.sessionID, b.agentID).Scan(&generation, &now, &expires, &account, &agent, &profile)
	if e != nil {
		return out, ar.ErrUnavailable
	}
	if b.sessionID != originalSession || generation != originalGeneration {
		return out, ar.ErrDenied
	}
	frame := ar.CursorFrame{Schema: ar.PageSchema, Owner: b.accountID, Agent: b.agentID, Session: b.sessionID, Generation: generation, Authority: liveDigestBytes("birdtie.run-page.authority.v1", []byte(account+":"+agent+":"+profile)), Ceiling: now.UTC().Truncate(time.Microsecond)}
	frame.Until = frame.Ceiling.Add(ar.PageLease)
	if expires.Before(frame.Until) {
		frame.Until = expires.UTC().Truncate(time.Microsecond)
	}
	var old ar.CursorFrame
	if proof != nil {
		old = proof.frame
	} else if cursor != "" {
		old, e = runPageCodec.Open(cursor)
		if e != nil {
			return out, e
		}
	}
	if proof != nil || cursor != "" {
		if old.Owner != frame.Owner || old.Agent != frame.Agent || old.Session != frame.Session || old.Generation != generation || old.Authority != frame.Authority || !old.Until.After(now) || old.Ceiling.After(now) || !old.Valid() {
			return out, ar.ErrConflict
		}
		frame = old
	}
	q := `SELECT ` + runColumns + ` FROM agent_enrichment_runs r WHERE r.owner_id=$1 AND r.agent_id=$2 AND r.created_at<=$3`
	args := []any{b.accountID, b.agentID, frame.Ceiling}
	if frame.ID != "" {
		q += ` AND (r.created_at<$4 OR(r.created_at=$4 AND r.id>$5::uuid))`
		args = append(args, frame.After, frame.ID)
	}
	q += ` ORDER BY r.created_at DESC,r.id LIMIT 21 FOR SHARE`
	rows, e := tx.Query(ctx, q, args...)
	if e != nil {
		return out, ar.ErrUnavailable
	}
	items := []ar.Record{}
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		items = append(items, r.record)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, ar.ErrUnavailable
	}
	more := len(items) > ar.PageSize
	if more {
		items = items[:ar.PageSize]
	}
	// Session SHARE is acquired only after metadata and selected Run waits.
	var alive bool
	var finalGeneration int64
	e = tx.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE id=$1 FOR SHARE`, b.sessionID).Scan(&finalGeneration)
	if e != nil {
		return out, ar.ErrDenied
	}
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT n.at,EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id JOIN agents ag ON ag.id=$3 AND ag.principal_account_id=a.id AND ag.status='active' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE s.id=$1 AND a.id=$2 AND a.account_type='person' AND a.status='active' AND s.authorization_generation=$4 AND s.revoked_at IS NULL AND s.expires_at>n.at AND s.idle_expires_at>n.at AND $5::timestamptz>n.at AND ($6::boolean OR s.authentication_method<>'dev_phone')) FROM n`, b.sessionID, b.accountID, b.agentID, frame.Generation, frame.Until, s.store.devPhoneEnabled).Scan(&now, &alive)
	if e != nil || ctx.Err() != nil {
		return out, ar.ErrUnavailable
	}
	if !alive || finalGeneration != frame.Generation {
		return out, ar.ErrDenied
	}
	out = ar.Page{Schema: ar.PageSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, Items: items, ObservedAt: now.UTC(), ExpiresAt: frame.Until}
	for i := range out.Items {
		out.Items[i].ObservedAt = out.ObservedAt
	}
	if more {
		next := frame
		last := items[len(items)-1]
		next.After = last.CreatedAt.UTC()
		next.ID = last.ID
		v, e := runPageCodec.Seal(next)
		if e != nil {
			return out, e
		}
		out.NextCursor = &v
	}
	if proof != nil {
		if now.Before(proof.frame.Ceiling) {
			return out, ar.ErrConflict
		}
	}
	if ar.ValidatePage(out, b.accountID, now) != nil {
		return ar.Page{}, ar.ErrUnavailable
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return ar.Page{}, ar.ErrUnavailable
	}
	out.Proof = &runPageProof{service: s, frame: frame, wire: runPageWire(out)}
	return out, nil
}
func (s *AgentRuns) ReadOwnRunPage(ctx context.Context, a agentprofile.PrivateAccess, cursor string) (ar.Page, error) {
	return s.runPageTx(ctx, a, cursor, nil)
}
func (s *AgentRuns) RevalidateOwnRunPage(ctx context.Context, a agentprofile.PrivateAccess, p ar.Page) error {
	proof, ok := p.Proof.(*runPageProof)
	if !ok || proof == nil || proof.service != s || proof.wire != runPageWire(p) {
		return ar.ErrConflict
	}
	next, e := s.runPageTx(ctx, a, "", proof)
	if e != nil {
		return e
	}
	// Re-reading cannot renew the original wire's lease or observation.
	next.ObservedAt = p.ObservedAt
	next.NextCursor = p.NextCursor
	for i := range next.Items {
		next.Items[i].ObservedAt = p.ObservedAt
	}
	if proof.wire != runPageWire(next) {
		return ar.ErrConflict
	}
	return nil
}
