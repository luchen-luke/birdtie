package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	review "github.com/birdtie/birdtie/apps/api/internal/agentmemoryreview"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"sync"
	"time"
)

// Process-local cursor key: a restart intentionally expires all cursors.
// Only read again from page one; no cursor can renew a session or approval.
var memoryReviewKey struct {
	once sync.Once
	key  [32]byte
	err  error
}

type memoryReviewCursor struct {
	Schema, Collection, Owner, Agent, Session, Authority string
	Generation                                           int64
	Ceiling, After, Until                                time.Time
	ID                                                   string
	Rank                                                 int
}

func encodeMemoryReviewCursor(c memoryReviewCursor) (string, error) {
	memoryReviewKey.once.Do(func() { _, memoryReviewKey.err = rand.Read(memoryReviewKey.key[:]) })
	if memoryReviewKey.err != nil {
		return "", agentmemory.ErrUnavailable
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return "", agentmemory.ErrUnavailable
	}
	m := hmac.New(sha256.New, memoryReviewKey.key[:])
	m.Write(raw)
	v := base64.RawURLEncoding.EncodeToString(append(raw, m.Sum(nil)...))
	if !review.CursorShape(v) {
		return "", agentmemory.ErrUnavailable
	}
	return v, nil
}
func decodeMemoryReviewCursor(v string) (c memoryReviewCursor, e error) {
	if !review.CursorShape(v) {
		return c, agentmemory.ErrInvalid
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(v)
	if e != nil || len(b) <= sha256.Size || base64.RawURLEncoding.EncodeToString(b) != v {
		return c, agentmemory.ErrInvalid
	}
	memoryReviewKey.once.Do(func() { _, memoryReviewKey.err = rand.Read(memoryReviewKey.key[:]) })
	if memoryReviewKey.err != nil {
		return c, agentmemory.ErrUnavailable
	}
	raw, signature := b[:len(b)-sha256.Size], b[len(b)-sha256.Size:]
	m := hmac.New(sha256.New, memoryReviewKey.key[:])
	m.Write(raw)
	if !hmac.Equal(signature, m.Sum(nil)) {
		return c, agentmemory.ErrConflict
	}
	if json.Unmarshal(raw, &c) != nil || c.Schema != review.Schema || c.ID == "" || c.After.IsZero() || c.Ceiling.IsZero() || c.Until.IsZero() || c.Generation < 1 || c.Rank < 0 || c.Rank > 1 {
		return c, agentmemory.ErrConflict
	}
	return c, nil
}
func memoryReviewBinding(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, dev bool) (b agentPrivateBinding, e error) {
	e = tx.QueryRow(ctx, `SELECT s.id,a.id,ag.id FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' JOIN sessions s ON s.account_id=a.id WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND s.token_sha256=$2 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp() AND ($3::boolean OR s.authentication_method<>'dev_phone') FOR SHARE OF a,ag`, a.WorkspacePrincipal.ID, a.SessionDigest[:], dev).Scan(&b.sessionID, &b.accountID, &b.agentID)
	if errors.Is(e, pgx.ErrNoRows) {
		return b, agentmemory.ErrForbidden
	}
	if e != nil {
		return b, memoryError(e)
	}
	return b, nil
}
func memoryReviewFinal(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, c memoryReviewCursor) error {
	var generation int64
	var valid bool
	e := tx.QueryRow(ctx, `SELECT authorization_generation,revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND $2::timestamptz>clock_timestamp() FROM sessions WHERE id=$1 FOR SHARE`, b.sessionID, c.Until).Scan(&generation, &valid)
	if e != nil {
		return memoryError(e)
	}
	if !valid || generation != c.Generation {
		return agentmemory.ErrForbidden
	}
	return nil
}
func memoryReviewFrame(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, kind, cursor string) (frame memoryReviewCursor, now time.Time, e error) {
	frame = memoryReviewCursor{Schema: review.Schema, Collection: kind, Owner: b.accountID, Agent: b.agentID, Session: b.sessionID}
	var expires time.Time
	var account, agent, profile string
	e = tx.QueryRow(ctx, `SELECT s.authorization_generation,clock_timestamp(),LEAST(s.expires_at,s.idle_expires_at),a.xmin::text,ag.xmin::text,ap.xmin::text FROM sessions s JOIN accounts a ON a.id=s.account_id JOIN agents ag ON ag.id=$2 AND ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE s.id=$1`, b.sessionID, b.agentID).Scan(&frame.Generation, &now, &expires, &account, &agent, &profile)
	if e != nil {
		return frame, now, memoryError(e)
	}
	frame.Authority = liveDigestBytes("birdtie.memory-review.authority.v1", []byte(account+":"+agent+":"+profile))
	frame.Ceiling = now.UTC().Truncate(time.Microsecond)
	frame.Until = frame.Ceiling.Add(review.Lease)
	if expires.Before(frame.Until) {
		frame.Until = expires.UTC().Truncate(time.Microsecond)
	}
	if cursor != "" {
		old, e := decodeMemoryReviewCursor(cursor)
		if e != nil {
			return frame, now, e
		}
		if old.Collection != kind || old.Owner != frame.Owner || old.Agent != frame.Agent || old.Session != frame.Session || old.Generation != frame.Generation || old.Authority != frame.Authority || !old.Until.After(now) || old.Ceiling.After(now) || old.Until.After(old.Ceiling.Add(review.Lease)) {
			return frame, now, agentmemory.ErrConflict
		}
		frame = old
	}
	return frame, now, nil
}
func memoryReviewNext(c memoryReviewCursor, more bool, created time.Time, id string, rank int) (*string, error) {
	if !more {
		return nil, nil
	}
	c.After = created.UTC()
	c.ID = id
	c.Rank = rank
	v, e := encodeMemoryReviewCursor(c)
	if e != nil {
		return nil, e
	}
	return &v, nil
}
func (s *Store) ReadOwnMemoryPage(ctx context.Context, a agentprofile.PrivateAccess, cursor string) (out review.MemoryPage, e error) {
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil || agentprofile.ValidatePrivateAccess(a) != nil {
		return out, agentmemory.ErrForbidden
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, agentmemory.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	b, e := memoryReviewBinding(ctx, tx, a, s.devPhoneEnabled)
	if e != nil {
		return out, memoryError(e)
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, false); e != nil {
		return out, memoryError(e)
	}
	frame, now, e := memoryReviewFrame(ctx, tx, b, review.Memories, cursor)
	if e != nil {
		return out, e
	}
	rank := `CASE WHEN source_type='EXPLICIT' THEN 0 ELSE 1 END`
	q := `SELECT ` + agentMemoryColumns + ` FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND status<>'DELETED' AND created_at<=$3`
	args := []any{b.agentID, b.accountID, frame.Ceiling}
	if cursor != "" {
		q += ` AND (` + rank + `>$4 OR (` + rank + `=$4 AND (created_at<$5 OR (created_at=$5 AND id>$6::uuid))))`
		args = append(args, frame.Rank, frame.After, frame.ID)
	}
	q += ` ORDER BY ` + rank + `,created_at DESC,id LIMIT 21 FOR SHARE`
	rows, e := tx.Query(ctx, q, args...)
	if e != nil {
		return out, agentmemory.ErrUnavailable
	}
	items := []agentmemory.Record{}
	for rows.Next() {
		r, e := scanAgentMemory(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		items = append(items, r)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, agentmemory.ErrUnavailable
	}
	more := len(items) > review.PageSize
	if more {
		items = items[:review.PageSize]
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	for i := range items {
		if !items[i].ValidUntil.After(now) {
			items[i].Status = agentmemory.StatusExpired
		}
	}
	out = review.MemoryPage{Schema: review.Schema, Collection: review.Memories, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, Items: items, ObservedAt: now.UTC(), ExpiresAt: frame.Until.UTC()}
	if more {
		last := items[len(items)-1]
		r := 1
		if last.SourceType == agentmemory.SourceExplicit {
			r = 0
		}
		out.NextCursor, e = memoryReviewNext(frame, true, last.CreatedAt, last.ID, r)
		if e != nil {
			return out, e
		}
	}
	if e = review.Validate(out, b.accountID, review.Memories, now); e != nil {
		return out, agentmemory.ErrConflict
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return out, memoryError(e)
	}
	if e = memoryReviewFinal(ctx, tx, b, frame); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	return out, nil
}
func (g *memoryCandidateHumanGateway) ReadOwnCandidatePage(ctx context.Context, a agentprofile.PrivateAccess, cursor string) (review.CandidatePage, error) {
	if g == nil || g.service == nil {
		return review.CandidatePage{}, agentmemory.ErrUnavailable
	}
	return g.service.ReadOwnCandidatePage(ctx, a, cursor)
}
func (s *MemoryCandidateService) ReadOwnCandidatePage(ctx context.Context, a agentprofile.PrivateAccess, cursor string) (out review.CandidatePage, e error) {
	stage := "begin"
	defer func() {
		if e != nil {
			e = fmt.Errorf("candidate review page %s: %w", stage, e)
		}
	}()
	if ctx == nil {
		return out, agentmemory.ErrInvalid
	}
	if s == nil || s.store == nil || s.store.pool == nil || agentprofile.ValidatePrivateAccess(a) != nil {
		return out, agentmemory.ErrForbidden
	}
	ticket, e := s.flags.Capture(agentfeature.Memory)
	if e != nil {
		return out, agentmemory.ErrUnavailable
	}
	tx, e := s.store.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	if e = memoryCorrectionGate(ctx, tx); e != nil {
		return out, e
	}
	b, e := memoryReviewBinding(ctx, tx, a, s.store.devPhoneEnabled)
	if e != nil {
		return out, e
	}
	metadata, e := lockAgentPrivateMetadata(ctx, tx, b, true)
	if e != nil {
		return out, memoryError(e)
	}
	meta := metadata.ProfileVersion
	stage = "frame"
	frame, now, e := memoryReviewFrame(ctx, tx, b, review.Candidates, cursor)
	if e != nil {
		return out, e
	}
	q := `SELECT ` + memoryCandidateColumns + ` FROM agent_memory_candidates WHERE agent_id=$1 AND owner_id=$2 AND created_at<=$3`
	args := []any{b.agentID, b.accountID, frame.Ceiling}
	if cursor != "" {
		q += ` AND (created_at<$4 OR (created_at=$4 AND id>$5::uuid))`
		args = append(args, frame.After, frame.ID)
	}
	q += ` ORDER BY created_at DESC,id LIMIT 21 FOR UPDATE`
	stage = "selection"
	rows, e := tx.Query(ctx, q, args...)
	if e != nil {
		return out, agentmemory.ErrUnavailable
	}
	states := []candidateState{}
	for rows.Next() {
		c, e := scanMemoryCandidate(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		states = append(states, c)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, agentmemory.ErrUnavailable
	}
	more := len(states) > review.PageSize
	if more {
		states = states[:review.PageSize]
	}
	items := []agentmemorycandidate.Record{}
	stage = "refresh"
	for _, state := range states {
		state, e = candidateRefresh(ctx, tx, b, meta, state)
		if e != nil {
			return out, e
		}
		items = append(items, agentmemorycandidate.Clone(state.record))
	}
	stage = "current sources"
	ids, e := candidateReviewCurrent(ctx, tx, b, items)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	out = review.CandidatePage{Schema: review.Schema, Collection: review.Candidates, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, Items: items, ObservedAt: now.UTC(), ExpiresAt: frame.Until.UTC()}
	stage = "cursor"
	if more {
		last := items[len(items)-1]
		out.NextCursor, e = memoryReviewNext(frame, true, last.CreatedAt, last.ID, 0)
		if e != nil {
			return out, e
		}
	}
	if e = review.Validate(out, b.accountID, review.Candidates, now); e != nil {
		return out, agentmemory.ErrConflict
	}
	stage = "last session"
	if e = memoryReviewFinal(ctx, tx, b, frame); e != nil {
		return out, e
	}
	stage = "finish"
	if e = s.finish(ctx, tx, b, ticket, &frame.Until, ids...); e != nil {
		return out, e
	}
	return out, nil
}
