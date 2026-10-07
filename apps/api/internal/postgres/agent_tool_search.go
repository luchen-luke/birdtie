package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
)

var _ agenttool.CurrentSearchPort = (*Store)(nil)

func currentReadSeal(domain, digest string, d agenttool.Decision, sourceSeal string) (string, error) {
	// Original source reader initializes the same cryptographically random key.
	resultProjectionKey.Do(func() { _, resultProjectionKey.err = rand.Read(resultProjectionKey.key[:]) })
	if resultProjectionKey.err != nil {
		return "", agenttool.ErrUnavailable
	}
	raw, e := json.Marshal(struct {
		Domain, Digest string
		Decision       agenttool.Decision
		SourceSeal     string
	}{domain, digest, d, sourceSeal})
	if e != nil {
		return "", agenttool.ErrUnavailable
	}
	h := hmac.New(sha256.New, resultProjectionKey.key[:])
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func currentReadError(e error) error {
	switch {
	case errors.Is(e, arp.ErrInvalid):
		return agenttool.ErrInvalid
	case errors.Is(e, arp.ErrDenied):
		return agenttool.ErrDenied
	case errors.Is(e, arp.ErrChanged):
		return agenttool.ErrChanged
	case errors.Is(e, arp.ErrUnavailable):
		return agenttool.ErrUnavailable
	}
	return e
}
func (s *Store) ReadOwnCurrentSearch(ctx context.Context, q agenttool.CurrentSearch) (agenttool.CurrentSearchReceipt, error) {
	var out agenttool.CurrentSearchReceipt
	if !q.Valid() {
		return out, agenttool.ErrInvalid
	}
	tx, _, p, e := s.beginCurrentToolRead(ctx, q.Access.Actor, q.Access.SessionDigest, resultProjectionRelations)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	tool := agenttool.CurrentSearchTool(q.Query.Kind)
	if agenttool.Restrict(tool, actorref.Person, p.level).Denied {
		return out, agenttool.ErrDenied
	}
	source, e := s.captureAgentResultProjectionCurrentTx(ctx, tx, q.Access, q.Query, nil, p)
	if e != nil {
		return out, currentReadError(e)
	}
	source.Seal, e = resultProjectionSeal(q.Access, q.Query, source)
	if e != nil {
		return out, e
	}
	p.observed, p.until = source.ObservedAt, source.ValidUntil
	digest := agenttool.CurrentSearchDigest(q)
	action := decisionToolID(q.Access.TaskID, tool, q.Access.TaskID, source.Proof, digest)
	out.Decision = nativeToolDecision(tool, action, q.Access.TaskID, source.Proof, digest, p.owner, p.agent, p, agenttool.Allow, "CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY")
	out.Source = source
	out.Seal, e = currentReadSeal("birdtie.human-task-read.v1", digest, out.Decision, source.Seal)
	if e != nil || !out.Valid(q) {
		return agenttool.CurrentSearchReceipt{}, agenttool.ErrChanged
	}
	// No identity/policy/clock SQL occurs after the original final payload.
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return agenttool.CurrentSearchReceipt{}, agenttool.ErrUnavailable
	}
	return out, nil
}
func (s *Store) RevalidateOwnCurrentSearch(ctx context.Context, q agenttool.CurrentSearch, r agenttool.CurrentSearchReceipt) error {
	if !r.Valid(q) {
		return agenttool.ErrDenied
	}
	seal, e := currentReadSeal("birdtie.human-task-read.v1", agenttool.CurrentSearchDigest(q), r.Decision, r.Source.Seal)
	if e != nil || !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return agenttool.ErrDenied
	}
	sourceSeal, e := resultProjectionSeal(q.Access, q.Query, r.Source)
	if e != nil || !hmac.Equal([]byte(sourceSeal), []byte(r.Source.Seal)) {
		return agenttool.ErrDenied
	}
	next, e := s.ReadOwnCurrentSearch(ctx, q)
	if e != nil {
		return e
	}
	if r.Decision.AgentID != next.Decision.AgentID || r.Decision.PolicyVersion != next.Decision.PolicyVersion || !unchangedOutputProjection(r.Source, next.Source) || !r.Decision.ExpiresAt.After(next.Source.ObservedAt) {
		return agenttool.ErrChanged
	}
	return nil
}
