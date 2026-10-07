package postgres

import (
	"context"
	"crypto/hmac"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"reflect"
)

var _ agenttool.CurrentMatchPort = (*Store)(nil)

// These parameters are private fields resolved from the native owner/session,
// never an actor label, model flag or a second permission/consent ledger.
func currentMatchPolicyFence(p *nativeToolPolicy) (string, string) {
	return `,(SELECT at FROM stamp)`, ` AND EXISTS(SELECT 1 FROM stamp WHERE stamp.at<$6::timestamptz)
 AND (SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(tp)||jsonb_build_object('_xmin',tp.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings tp WHERE tp.owner_id=$8::uuid AND tp.agent_id=$9::uuid AND tp.owner_type='PERSON')=$7::text
 AND EXISTS(SELECT 1 FROM agents current_agent JOIN agent_profiles current_profile ON current_profile.agent_id=current_agent.id AND current_profile.owner_type='PERSON' AND current_profile.owner_id=$8::uuid WHERE current_agent.id=$9::uuid AND current_agent.principal_account_id=$8::uuid AND current_agent.agent_type='personal' AND current_agent.status='active')`
}
func (s *Store) ReadOwnCurrentMatch(ctx context.Context, q agenttool.CurrentMatch) (agenttool.CurrentMatchReceipt, error) {
	var out agenttool.CurrentMatchReceipt
	if !q.Valid() {
		return out, agenttool.ErrInvalid
	}
	tx, _, p, e := s.beginCurrentToolRead(ctx, q.Actor, q.SessionDigest, humanNewPeopleRelations)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	if agenttool.Restrict(agenttool.PersonMatch, actorref.Person, p.level).Denied {
		return out, agenttool.ErrDenied
	}
	out.Source, e = s.humanNewPeopleReadTx(ctx, tx, q.Actor, q.SessionDigest, q.SourceIntentID, p, &out.ObservedAt)
	if e != nil {
		return out, e
	}
	out.Source.ExpiresAt = retryMinimum(out.Source.ExpiresAt, p.until)
	p.observed, p.until = out.ObservedAt, out.Source.ExpiresAt
	out.Source.Seal, e = humanNewPeopleSeal(q.Actor, q.SessionDigest, out.Source)
	if e != nil {
		return out, agenttool.ErrUnavailable
	}
	digest := agenttool.CurrentMatchDigest(q)
	action := decisionToolID(q.SourceIntentID, agenttool.PersonMatch, q.SourceIntentID, out.Source.Proof, digest)
	out.Decision = nativeToolDecision(agenttool.PersonMatch, action, q.SourceIntentID, out.Source.Proof, digest, p.owner, p.agent, p, agenttool.Allow, "CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY")
	out.Seal, e = currentReadSeal("birdtie.human-match-read.v1", digest, out.Decision, out.Source.Seal)
	if e != nil || !out.Valid(q) {
		return agenttool.CurrentMatchReceipt{}, agenttool.ErrChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return agenttool.CurrentMatchReceipt{}, agenttool.ErrUnavailable
	}
	return out, nil
}
func (s *Store) RevalidateOwnCurrentMatch(ctx context.Context, q agenttool.CurrentMatch, r agenttool.CurrentMatchReceipt) error {
	if !r.Valid(q) {
		return agenttool.ErrDenied
	}
	seal, e := currentReadSeal("birdtie.human-match-read.v1", agenttool.CurrentMatchDigest(q), r.Decision, r.Source.Seal)
	if e != nil || !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return agenttool.ErrDenied
	}
	sourceSeal, e := humanNewPeopleSeal(q.Actor, q.SessionDigest, r.Source)
	if e != nil || !hmac.Equal([]byte(sourceSeal), []byte(r.Source.Seal)) {
		return agenttool.ErrDenied
	}
	next, e := s.ReadOwnCurrentMatch(ctx, q)
	if e != nil {
		return e
	}
	if r.Decision.AgentID != next.Decision.AgentID || r.Decision.PolicyVersion != next.Decision.PolicyVersion || r.Source.Proof != next.Source.Proof || !reflect.DeepEqual(r.Source.Response, next.Source.Response) || !r.Source.ExpiresAt.After(next.ObservedAt) {
		return agenttool.ErrChanged
	}
	return nil
}
