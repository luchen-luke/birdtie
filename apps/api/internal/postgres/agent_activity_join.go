package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

// trustedEnabled is startup configuration, never a client setting or grant.
// The original Enrichment controller remains an independent rollout brake.
type AgentActivityJoin struct {
	store   *Store
	flags   *agentfeature.Controller
	enabled atomic.Bool
}

func NewAgentActivityJoin(s *Store, c *agentfeature.Controller, trustedEnabled bool) *AgentActivityJoin {
	g := &AgentActivityJoin{store: s, flags: c}
	g.enabled.Store(trustedEnabled)
	return g
}
func (g *AgentActivityJoin) Disable() {
	if g != nil {
		g.enabled.Store(false)
	}
}

var _ aa.ActivityJoinHumanGateway = (*AgentActivityJoin)(nil)

func (g *AgentActivityJoin) gate(ctx context.Context) (agentfeature.Ticket, error) {
	if g == nil || g.store == nil || g.store.pool == nil || g.flags == nil || !g.enabled.Load() || ctx == nil || ctx.Err() != nil {
		return agentfeature.Ticket{}, aa.ErrUnavailable
	}
	return actionGate(ctx, g.flags)
}
func (g *AgentActivityJoin) fresh(ctx context.Context, t agentfeature.Ticket) error {
	if ctx == nil || ctx.Err() != nil || g == nil || !g.enabled.Load() || !g.flags.Current(t) {
		return aa.ErrChanged
	}
	return nil
}
func (g *AgentActivityJoin) commit(ctx context.Context, tx pgx.Tx, t agentfeature.Ticket) error {
	if e := g.fresh(ctx, t); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return aa.ErrUnknown
	}
	if e := g.fresh(ctx, t); e != nil {
		return aa.ErrUnknown
	}
	return nil
}
func activityJoinError(e error) error {
	if e == nil {
		return nil
	}
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		return aa.ErrDenied
	case errors.Is(e, ea.ErrChanged):
		return aa.ErrChanged
	case errors.Is(e, ea.ErrNotFound):
		return aa.ErrDenied
	case errors.Is(e, ea.ErrInvalid):
		return aa.ErrInvalid
	}
	return e
}
func (g *AgentActivityJoin) begin(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, agentfeature.Ticket, error) {
	return g.beginMode(ctx, a, true)
}
func (g *AgentActivityJoin) beginRead(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, agentfeature.Ticket, error) {
	return g.beginMode(ctx, a, false)
}
func (g *AgentActivityJoin) beginMode(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, agentfeature.Ticket, error) {
	var t agentfeature.Ticket
	var e error
	if write {
		t, e = g.gate(ctx)
		if e != nil {
			return nil, t, e
		}
	} else if g == nil || g.store == nil || g.store.pool == nil || ctx == nil || ctx.Err() != nil {
		return nil, t, aa.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil || a.WorkspacePrincipal.Type != actorref.Person {
		return nil, t, aa.ErrDenied
	}
	tx, e := g.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, t, aa.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, agentfeature.Ticket, error) {
		tx.Rollback(context.Background())
		return nil, t, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';SET LOCAL jit=off;LOCK TABLE agent_action_approvals,agent_action_dispatches,agent_activity_join_effects,consent_grants,audit_events IN ROW EXCLUSIVE MODE`); e != nil {
		return fail(aa.ErrUnavailable)
	}
	var triggers int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class r ON r.oid=t.tgrelid JOIN pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname='public' AND t.tgenabled='O' AND NOT t.tgisinternal AND (t.tgname NOT IN('agent_action_atomic_guard','agent_activity_join_effect_atomic_guard') OR(t.tgdeferrable AND t.tginitdeferred)) AND ((r.relname='agent_action_approvals' AND t.tgname IN('agent_action_approval_guard','agent_action_atomic_guard')) OR(r.relname='agent_action_dispatches' AND t.tgname='agent_action_dispatch_guard') OR(r.relname='consent_grants' AND t.tgname='agent_action_grant_guard') OR(r.relname='agent_activity_join_effects' AND t.tgname IN('agent_activity_join_effect_guard','agent_activity_join_effect_truncate_guard','agent_activity_join_effect_atomic_guard')))`).Scan(&triggers); e != nil || triggers != 7 {
		return fail(aa.ErrUnavailable)
	}
	var replyGuard bool
	if e = tx.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_activity_join_reply_source_current(uuid)') IS NOT NULL`).Scan(&replyGuard); e != nil || !replyGuard {
		return fail(aa.ErrUnavailable)
	}
	// The legacy domain order is Session before Activity. Preserve it to avoid
	// acquiring a session after an Activity held by a waiting human writer.
	if e = g.store.lockEntityActionWriter(ctx, tx, joinHumanAccess(a)); e != nil {
		return fail(activityJoinError(e))
	}
	return tx, t, nil
}
func joinHumanAccess(a agentprofile.PrivateAccess) ea.Access {
	return ea.Access{Actor: identity.Actor{ID: a.WorkspacePrincipal.ID, AccountType: "person"}, SessionDigest: a.SessionDigest}
}

type joinApproval struct {
	b             aa.ActivityJoinBinding
	p             aa.ActivityJoinInput
	digest, state string
	consumed      bool
	review        *aa.ActivityJoinReview
}

func readJoinApproval(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, id string, lock bool) (joinApproval, error) {
	var p joinApproval
	var raw, payload string
	var approved, consumed, cancelled *time.Time
	q := `SELECT binding_canonical,binding_digest,payload_canonical,approved_at,consumed_at,cancelled_at FROM agent_action_approvals WHERE id=$1 AND owner_id=$2 AND action_kind='ACTIVITY_JOIN'`
	if lock {
		q += ` FOR UPDATE`
	}
	if e := tx.QueryRow(ctx, q, id, a.WorkspacePrincipal.ID).Scan(&raw, &p.digest, &payload, &approved, &consumed, &cancelled); e != nil {
		return p, actionError(e)
	}
	if json.Unmarshal([]byte(raw), &p.b) != nil || json.Unmarshal([]byte(payload), &p.p) != nil || !aa.ValidActivityJoinBinding(p.b, p.p) || aa.ActivityJoinBindingDigest(raw) != p.digest {
		return p, aa.ErrUnavailable
	}
	p.state = aa.Pending
	if approved != nil {
		p.state = aa.Approved
	}
	if consumed != nil {
		p.state = aa.Consumed
		p.consumed = true
	}
	if cancelled != nil {
		p.state = aa.Cancelled
	}
	return p, nil
}
func joinPreview(p joinApproval) aa.ActivityJoinPreview {
	return aa.ActivityJoinPreview{SchemaVersion: aa.ActivityJoinSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: p.b.ActorID}, AgentID: p.b.AgentID, TaskID: p.b.TaskID, ApprovalID: p.b.ApprovalID, LogicalOperationID: p.b.LogicalOperationID, ActionID: p.b.ActionID, ActivityID: p.b.TargetID, BindingDigest: p.digest, State: p.state, ObservedAt: p.b.ObservedAt, ExpiresAt: p.b.ExpiresAt, DispatchCommitted: p.consumed, Review: p.review}
}
func (g *AgentActivityJoin) source(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, id string) (ea.Receipt, error) {
	var r ea.Receipt
	var live, visible bool
	var raw []byte
	if e := tx.QueryRow(ctx, `SELECT id FROM activities WHERE id=$1 AND visibility='public' FOR UPDATE`, id).Scan(new(string)); e != nil {
		return r, aa.ErrDenied
	}
	e := tx.QueryRow(ctx, entityActionSQL("activity"), id, a.WorkspacePrincipal.ID, a.SessionDigest[:], g.store.devPhoneEnabled, "activity", false).Scan(&r.View.ObservedAt, &r.View.ValidUntil, &live, &visible, &r.View.Title, &raw, &r.Proof)
	if e != nil {
		return r, aa.ErrUnavailable
	}
	if !live || !visible {
		return r, aa.ErrDenied
	}
	var facts ea.Facts
	if json.Unmarshal(raw, &facts) != nil {
		return r, aa.ErrUnavailable
	}
	r.View = ea.Project(ea.Ref{Type: "activity", ID: id}, facts, r.View.ObservedAt, r.View.ValidUntil, r.View.Title, r.Proof)
	allowed := false
	for _, d := range r.View.Actions {
		if d.Kind == ea.Join && d.State == ea.Available {
			for _, op := range d.AllowedOperations {
				allowed = allowed || op == "JOIN"
			}
		}
	}
	if !r.View.Valid() || !allowed {
		return r, aa.ErrChanged
	}
	return r, nil
}
func (g *AgentActivityJoin) current(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, p joinApproval, source bool) (entityActionWriteFence, error) {
	var f entityActionWriteFence
	s, e := g.captureJoinReply(ctx, tx, a, p.p)
	if e != nil {
		return f, e
	}
	if !joinReplyBindingMatches(p, s) {
		return f, aa.ErrChanged
	}
	var gen, authority string
	if e = tx.QueryRow(ctx, `SELECT t.xmin::text,birdtie_activity_join_authority($1,$2,$3) FROM agent_tasks t WHERE id=$4`, p.b.SessionID, p.b.ActorID, p.b.AgentID, p.b.TaskID).Scan(&gen, &authority); e != nil {
		return f, aa.ErrUnavailable
	}
	if gen != p.b.SourceGeneration || authority != p.b.AuthorityVersion {
		return f, aa.ErrChanged
	}
	if p.state != aa.Pending {
		var grantLive bool
		if e = tx.QueryRow(ctx, `SELECT revision=1 AND revoked_at IS NULL AND expires_at>clock_timestamp() FROM consent_grants WHERE id=$1 AND owner_account_id=$2 AND recipient_account_id=$2 AND resource_type='agent_context' AND resource_id=$3 AND purpose='OWN_ACTIVITY_JOIN_ACTION' AND actions=ARRAY['activity_join']::text[] FOR SHARE`, p.b.GrantID, p.b.ActorID, p.b.ApprovalID).Scan(&grantLive); e != nil || !grantLive {
			return f, aa.ErrDenied
		}
	}
	if source {
		f, e = g.store.checkEntityActionWrite(ctx, tx, joinHumanAccess(a), ea.Ref{Type: "activity", ID: p.b.TargetID}, ea.BoundCondition{SourceVersion: p.b.TargetVersion, ValidUntil: p.b.ExpiresAt, Kind: ea.Join, Operation: "JOIN"})
		if e != nil {
			return f, activityJoinError(e)
		}
	}
	return f, nil
}
func (g *AgentActivityJoin) final(ctx context.Context, tx pgx.Tx, p joinApproval, t agentfeature.Ticket, effect bool) error {
	var live bool
	fn := `SELECT birdtie_activity_join_current($1)`
	if effect {
		fn = `SELECT birdtie_activity_join_effect_current($1)`
	}
	if e := tx.QueryRow(ctx, fn, p.b.ApprovalID).Scan(&live); e != nil {
		return aa.ErrUnavailable
	}
	if !live {
		return aa.ErrExpired
	}
	return g.fresh(ctx, t)
}

type joinProof struct {
	origin    *AgentActivityJoin
	access    agentprofile.PrivateAccess
	hash      [32]byte
	authority string
}

func joinWire(v any) [32]byte { raw, _ := json.Marshal(v); return sha256.Sum256(raw) }
func (g *AgentActivityJoin) previewProof(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, v aa.ActivityJoinPreview) (aa.ActivityJoinPreview, error) {
	var authority string
	if e := tx.QueryRow(ctx, `SELECT birdtie_activity_join_reader_authority($1,$2,$3)`, a.SessionDigest[:], a.WorkspacePrincipal.ID, v.AgentID).Scan(&authority); e != nil {
		return v, aa.ErrDenied
	}
	v.Proof = &joinProof{g, a, joinWire(v), authority}
	return v, nil
}

func (g *AgentActivityJoin) PreviewOwn(ctx context.Context, a agentprofile.PrivateAccess, input aa.ActivityJoinInput) (aa.ActivityJoinPreview, error) {
	tx, t, e := g.begin(ctx, a)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	defer tx.Rollback(context.Background())
	if !input.Valid() || !input.CurrentSourceValid() {
		return aa.ActivityJoinPreview{}, aa.ErrInvalid
	}
	s, e := g.captureJoinReply(ctx, tx, a, input)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	cur, source, policy := s.current, s.target, s.policy
	id, e := actionUUID()
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	grant, e := actionUUID()
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	p := joinApproval{p: input, state: aa.Pending, review: s.review}
	p.b = aa.ActivityJoinBinding{Binding: aa.Binding{Schema: aa.ActivityJoinSchema, ApprovalID: id, TenantID: a.WorkspacePrincipal.ID, ActorID: a.WorkspacePrincipal.ID, SubjectType: "PERSON", SubjectID: a.WorkspacePrincipal.ID, AgentID: cur.agent.AgentID, SessionID: cur.sessionID, TaskID: input.TaskID, LogicalOperationID: input.LogicalOperationID, ActionID: input.ActionID, Tool: aa.ActivityJoinTool, ToolVersion: aa.ActivityJoinToolVersion, TargetID: input.ActivityID, PayloadDigest: aa.ActivityJoinPayloadDigest(input), SourceVersion: cur.version.Token, PolicyVersion: policy.token, ConsentPurpose: aa.ActivityJoinPurpose, GrantID: grant, GrantRevision: 1, MembershipVersion: "NONE_PERSON_ONLY"}, TargetVersion: source.Proof}
	p.b.SourceContract = aa.ActivityJoinReplySource
	p.b.ReplyIndex = strconv.Itoa(s.selector.index)
	p.b.ReplyTurnDigest = s.selector.membership.TurnDigest
	p.b.ReplyResultSetID = s.selector.membership.ResultSetID
	p.b.ReviewDigest = aa.ActivityJoinReviewDigest(*s.review)
	if e = tx.QueryRow(ctx, `SELECT t.xmin::text,birdtie_activity_join_authority($1,$2,$3),birdtie_activity_join_target_tail($4,$2),birdtie_activity_join_participant_version($4,$2),clock_timestamp(),LEAST($5::timestamptz,s.expires_at,s.idle_expires_at,clock_timestamp()+interval '30 seconds') FROM agent_tasks t JOIN sessions s ON s.id=$1 WHERE t.id=$6`, cur.sessionID, a.WorkspacePrincipal.ID, cur.agent.AgentID, input.ActivityID, retryMinimum(policy.until, s.until), input.TaskID).Scan(&p.b.SourceGeneration, &p.b.AuthorityVersion, &p.b.TargetTailVersion, &p.b.ParticipationVersion, &p.b.ObservedAt, &p.b.ExpiresAt); e != nil {
		return aa.ActivityJoinPreview{}, aa.ErrUnavailable
	}
	p.b.ObservedAt = p.b.ObservedAt.UTC()
	p.b.ExpiresAt = p.b.ExpiresAt.UTC()
	raw, digest, e := aa.ActivityJoinCanonical(p.b, input)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	p.digest = digest
	payload, _ := json.Marshal(input)
	if _, e = tx.Exec(ctx, `INSERT INTO agent_action_approvals(id,owner_id,agent_id,task_id,session_id,grant_id,binding_canonical,binding_digest,payload_canonical,observed_at,expires_at,action_kind)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'ACTIVITY_JOIN')`, id, p.b.ActorID, p.b.AgentID, p.b.TaskID, p.b.SessionID, grant, raw, digest, string(payload), p.b.ObservedAt, p.b.ExpiresAt); e != nil {
		return aa.ActivityJoinPreview{}, aa.ErrUnavailable
	}
	if e = activityJoinAudit(ctx, tx, p.b.ActorID, "activity_join_preview", id); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if _, e = g.current(ctx, tx, a, p, true); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if e = g.final(ctx, tx, p, t, false); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	view, e := g.previewForApproval(ctx, tx, a, p)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	v, e := g.previewProof(ctx, tx, a, view)
	if e != nil {
		return v, e
	}
	if e = g.commit(ctx, tx, t); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	return v, nil
}
func (g *AgentActivityJoin) ConfirmOwn(ctx context.Context, a agentprofile.PrivateAccess, id, digest string) (aa.ActivityJoinPreview, error) {
	tx, t, e := g.begin(ctx, a)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := readJoinApproval(ctx, tx, a, id, true)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if _, e = g.current(ctx, tx, a, p, true); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if digest != p.digest {
		return aa.ActivityJoinPreview{}, aa.ErrChanged
	}
	if p.state == aa.Consumed {
		return aa.ActivityJoinPreview{}, aa.ErrConsumed
	}
	if p.state == aa.Cancelled {
		return aa.ActivityJoinPreview{}, aa.ErrDenied
	}
	if p.state == aa.Pending {
		if _, e = tx.Exec(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,revision,expires_at,created_at)VALUES($1,$2,$2,'agent_context',$3,'OWN_ACTIVITY_JOIN_ACTION',ARRAY['activity_join']::text[],1,$4,clock_timestamp())`, p.b.GrantID, p.b.ActorID, id, p.b.ExpiresAt); e != nil {
			return aa.ActivityJoinPreview{}, aa.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE agent_action_approvals SET approved_at=clock_timestamp() WHERE id=$1 AND approved_at IS NULL`, id); e != nil {
			return aa.ActivityJoinPreview{}, aa.ErrUnavailable
		}
		p.state = aa.Approved
		if e = activityJoinAudit(ctx, tx, p.b.ActorID, "activity_join_approve", id); e != nil {
			return aa.ActivityJoinPreview{}, e
		}
	}
	if e = g.final(ctx, tx, p, t, false); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	view, e := g.previewForApproval(ctx, tx, a, p)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	v, e := g.previewProof(ctx, tx, a, view)
	if e != nil {
		return v, e
	}
	if e = g.commit(ctx, tx, t); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	return v, nil
}
func (g *AgentActivityJoin) RevokeOwn(ctx context.Context, a agentprofile.PrivateAccess, id string) (aa.ActivityJoinPreview, error) {
	tx, _, e := g.beginRead(ctx, a)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := readJoinApproval(ctx, tx, a, id, true)
	if e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if e = g.reader(ctx, tx, a, p.b.AgentID); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1 AND purpose='OWN_ACTIVITY_JOIN_ACTION' AND revoked_at IS NULL`, p.b.GrantID); e != nil {
		return aa.ActivityJoinPreview{}, aa.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_action_approvals SET cancelled_at=clock_timestamp() WHERE id=$1 AND cancelled_at IS NULL`, id); e != nil {
		return aa.ActivityJoinPreview{}, aa.ErrUnavailable
	}
	p.state = aa.Cancelled
	if e = activityJoinAudit(ctx, tx, p.b.ActorID, "activity_join_revoke", id); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	if e = g.reader(ctx, tx, a, p.b.AgentID); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	v, e := g.previewProof(ctx, tx, a, joinPreview(p))
	if e != nil {
		return v, e
	}
	if e = joinReadCommit(ctx, tx); e != nil {
		return aa.ActivityJoinPreview{}, e
	}
	return v, nil
}
func (g *AgentActivityJoin) reader(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, agent string) error {
	var live bool
	if e := tx.QueryRow(ctx, `SELECT birdtie_activity_join_reader_authority($1,$2,$3) IS NOT NULL`, a.SessionDigest[:], a.WorkspacePrincipal.ID, agent).Scan(&live); e != nil {
		return aa.ErrUnavailable
	}
	if !live {
		return aa.ErrDenied
	}
	return nil
}
func (g *AgentActivityJoin) ValidatePreviewOwn(ctx context.Context, a agentprofile.PrivateAccess, v aa.ActivityJoinPreview) error {
	proof, ok := v.Proof.(*joinProof)
	if !ok || proof == nil || proof.origin != g || proof.access != a || proof.hash != joinWire(v) || aa.ValidateActivityJoinPreview(v, a.WorkspacePrincipal) != nil {
		return aa.ErrDenied
	}
	tx, t, e := g.beginMode(ctx, a, v.State != aa.Cancelled)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	p, e := readJoinApproval(ctx, tx, a, v.ApprovalID, false)
	if e != nil {
		return e
	}
	current, e := g.previewForApproval(ctx, tx, a, p)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(current, func() aa.ActivityJoinPreview { q := v; q.Proof = nil; return q }()) {
		return aa.ErrChanged
	}
	if p.state != aa.Cancelled {
		if _, e = g.current(ctx, tx, a, p, true); e != nil {
			return e
		}
		if e = g.final(ctx, tx, p, t, false); e != nil {
			return e
		}
	}
	var authority string
	if e = tx.QueryRow(ctx, `SELECT birdtie_activity_join_reader_authority($1,$2,$3)`, a.SessionDigest[:], a.WorkspacePrincipal.ID, p.b.AgentID).Scan(&authority); e != nil || authority != proof.authority {
		return aa.ErrDenied
	}
	if p.state == aa.Cancelled {
		return joinReadCommit(ctx, tx)
	}
	return g.commit(ctx, tx, t)
}
func joinReadCommit(ctx context.Context, tx pgx.Tx) error {
	if ctx.Err() != nil {
		return aa.ErrUnavailable
	}
	if e := tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return aa.ErrUnknown
	}
	return nil
}
func activityJoinAudit(ctx context.Context, tx pgx.Tx, owner, action, id string) error {
	switch action {
	case "activity_join_preview", "activity_join_approve", "activity_join_dispatch", "activity_join_begin", "activity_join_reconcile", "activity_join_revoke":
	default:
		return aa.ErrInvalid
	}
	_, e := auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose,target_resource_id)VALUES($1,$2,'agent_activity_join',$3::text,'allowed',CASE WHEN EXISTS(SELECT 1 FROM agent_action_approvals p LEFT JOIN agent_action_dispatches d ON d.approval_id=p.id WHERE(p.id=$3::uuid OR d.id=$3::uuid) AND p.owner_id=$1 AND p.action_kind='ACTIVITY_JOIN' AND p.binding_canonical::jsonb->>'source_contract'='LATEST_ACTIVITY_REPLY_V1') THEN 'OWN_ACTIVITY_JOIN_REPLY_ACTION' ELSE 'OWN_ACTIVITY_JOIN_ACTION' END,$3::uuid)`, owner, action, id)
	return actionError(e)
}
