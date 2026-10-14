package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"time"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
)

type joinCommitment struct {
	g    *AgentActivityJoin
	a    agentprofile.PrivateAccess
	p    joinApproval
	id   string
	t    agentfeature.Ticket
	used atomic.Bool
}
type joinClaim struct {
	g     *AgentActivityJoin
	a     agentprofile.PrivateAccess
	p     joinApproval
	id    string
	fence int64
	t     agentfeature.Ticket
	used  atomic.Bool
}

func (*joinCommitment) MarshalJSON() ([]byte, error) { return nil, aa.ErrServerOnly }
func (*joinCommitment) UnmarshalJSON([]byte) error   { return aa.ErrServerOnly }
func (*joinClaim) MarshalJSON() ([]byte, error)      { return nil, aa.ErrServerOnly }
func (*joinClaim) UnmarshalJSON([]byte) error        { return aa.ErrServerOnly }

func readJoinReceipt(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, id string, lock bool) (aa.ActivityJoinReceipt, error) {
	var r aa.ActivityJoinReceipt
	var owner, agent string
	q := `SELECT d.id,d.approval_id,d.state,d.committed_at,d.effect_id,d.applied_at,p.owner_id,p.agent_id FROM agent_action_dispatches d JOIN agent_action_approvals p ON p.id=d.approval_id WHERE d.id=$1 AND d.owner_id=$2 AND p.owner_id=$2 AND p.action_kind='ACTIVITY_JOIN'`
	if lock {
		q += ` FOR UPDATE OF d`
	}
	if e := tx.QueryRow(ctx, q, id, a.WorkspacePrincipal.ID).Scan(&r.DispatchID, &r.ApprovalID, &r.Status, &r.CommittedAt, &r.EffectID, &r.AppliedAt, &owner, &agent); e != nil {
		return r, actionError(e)
	}
	r.SchemaVersion = aa.ActivityJoinSchema
	r.Owner = a.WorkspacePrincipal
	r.AgentID = agent
	r.CommittedAt = r.CommittedAt.UTC()
	if r.AppliedAt != nil {
		u := r.AppliedAt.UTC()
		r.AppliedAt = &u
	}
	if r.Status == aa.Succeeded {
		var pid string
		var applied time.Time
		if e := tx.QueryRow(ctx, `SELECT participation_id,applied_at FROM agent_activity_join_effects WHERE id=$1 AND dispatch_id=$2 AND owner_id=$3 AND agent_id=$4`, r.EffectID, r.DispatchID, owner, agent).Scan(&pid, &applied); e != nil || r.AppliedAt == nil || !applied.Equal(*r.AppliedAt) {
			return r, aa.ErrUnknown
		}
		r.ParticipationID = &pid
	}
	if e := aa.ValidateActivityJoinReceipt(r, a.WorkspacePrincipal, r.ApprovalID); e != nil {
		return r, e
	}
	return r, nil
}
func (g *AgentActivityJoin) receiptProof(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, r aa.ActivityJoinReceipt) (aa.ActivityJoinReceipt, error) {
	var authority string
	if e := tx.QueryRow(ctx, `SELECT birdtie_activity_join_reader_authority($1,$2,$3)`, a.SessionDigest[:], a.WorkspacePrincipal.ID, r.AgentID).Scan(&authority); e != nil {
		return r, aa.ErrDenied
	}
	r.Proof = &joinProof{g, a, joinWire(r), authority}
	return r, nil
}

// Repeated Commit returns a historical view without a second capability.
func (g *AgentActivityJoin) CommitOwn(ctx context.Context, a agentprofile.PrivateAccess, id, digest string) (aa.ActivityJoinReceipt, aa.ActivityJoinCommitment, error) {
	tx, t, e := g.begin(ctx, a)
	if e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	defer tx.Rollback(context.Background())
	p, e := readJoinApproval(ctx, tx, a, id, true)
	if e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	if _, e = g.current(ctx, tx, a, p, true); e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	if p.digest != digest {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrChanged
	}
	if p.state == aa.Consumed {
		var did string
		if e = tx.QueryRow(ctx, `SELECT id FROM agent_action_dispatches WHERE approval_id=$1 AND owner_id=$2`, id, a.WorkspacePrincipal.ID).Scan(&did); e != nil {
			return aa.ActivityJoinReceipt{}, nil, aa.ErrUnknown
		}
		r, e := readJoinReceipt(ctx, tx, a, did, false)
		if e != nil {
			return r, nil, e
		}
		r, e = g.receiptProof(ctx, tx, a, r)
		if e != nil {
			return r, nil, e
		}
		if e = g.commit(ctx, tx, t); e != nil {
			return aa.ActivityJoinReceipt{}, nil, e
		}
		return r, nil, nil
	}
	if p.state != aa.Approved {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrApproval
	}
	var live bool
	if e = tx.QueryRow(ctx, `SELECT revision=1 AND revoked_at IS NULL AND expires_at>clock_timestamp() FROM consent_grants WHERE id=$1 AND owner_account_id=$2 AND recipient_account_id=$2 AND resource_type='agent_context' AND resource_id=$3 AND purpose='OWN_ACTIVITY_JOIN_ACTION' AND actions=ARRAY['activity_join']::text[] FOR UPDATE`, p.b.GrantID, p.b.ActorID, id).Scan(&live); e != nil || !live {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrDenied
	}
	key := aa.ActivityJoinEffectKey(p.b)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.activity-join.effect:'||$1,0))`, key); e != nil {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrUnavailable
	}
	var existing string
	e = tx.QueryRow(ctx, `SELECT id FROM agent_action_dispatches WHERE owner_id=$1 AND effect_key=$2`, p.b.ActorID, key).Scan(&existing)
	if e == nil {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrChanged
	}
	if e != pgx.ErrNoRows {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrUnavailable
	}
	did, e := actionUUID()
	if e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_action_approvals SET consumed_at=clock_timestamp() WHERE id=$1 AND consumed_at IS NULL`, id); e != nil {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO agent_action_dispatches(id,approval_id,owner_id,effect_key,state,committed_at)VALUES($1,$2,$3,$4,'DISPATCH_COMMITTED',clock_timestamp())`, did, id, p.b.ActorID, key); e != nil {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrUnavailable
	}
	if e = activityJoinAudit(ctx, tx, p.b.ActorID, "activity_join_dispatch", did); e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	if e = g.final(ctx, tx, p, t, false); e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	r, e := readJoinReceipt(ctx, tx, a, did, false)
	if e != nil {
		return r, nil, e
	}
	r, e = g.receiptProof(ctx, tx, a, r)
	if e != nil {
		return r, nil, e
	}
	if e = g.commit(ctx, tx, t); e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	return r, &joinCommitment{g: g, a: a, p: p, id: did, t: t}, nil
}
func (n *joinCommitment) Begin(ctx context.Context, a agentprofile.PrivateAccess) (aa.ActivityJoinReceipt, aa.ActivityJoinClaim, error) {
	if n == nil || n.g == nil || n.a != a || n.g.fresh(ctx, n.t) != nil || !n.used.CompareAndSwap(false, true) {
		return aa.ActivityJoinReceipt{}, nil, aa.ErrConsumed
	}
	tx, _, e := n.g.begin(ctx, a)
	if e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	defer tx.Rollback(context.Background())
	r, e := readJoinReceipt(ctx, tx, a, n.id, true)
	if e != nil {
		return r, nil, e
	}
	if r.Status != aa.Committed {
		return r, nil, aa.ErrConsumed
	}
	if _, e = n.g.current(ctx, tx, a, n.p, true); e != nil {
		return r, nil, e
	}
	if e = n.g.final(ctx, tx, n.p, n.t, false); e != nil {
		return r, nil, e
	}
	var fence int64
	if e = tx.QueryRow(ctx, `UPDATE agent_action_dispatches SET state='IN_FLIGHT',started_at=clock_timestamp(),lease_until=LEAST($2::timestamptz,clock_timestamp()+interval '30 seconds'),fence=fence+1 WHERE id=$1 AND state='DISPATCH_COMMITTED' AND $2>clock_timestamp() RETURNING fence`, n.id, n.p.b.ExpiresAt).Scan(&fence); e != nil {
		return r, nil, aa.ErrUnknown
	}
	if e = activityJoinAudit(ctx, tx, n.p.b.ActorID, "activity_join_begin", n.id); e != nil {
		return r, nil, e
	}
	if e = n.g.final(ctx, tx, n.p, n.t, false); e != nil {
		return r, nil, e
	}
	r, e = readJoinReceipt(ctx, tx, a, n.id, false)
	if e != nil {
		return r, nil, e
	}
	r, e = n.g.receiptProof(ctx, tx, a, r)
	if e != nil {
		return r, nil, e
	}
	if e = n.g.commit(ctx, tx, n.t); e != nil {
		return aa.ActivityJoinReceipt{}, nil, e
	}
	return r, &joinClaim{g: n.g, a: a, p: n.p, id: n.id, fence: fence, t: n.t}, nil
}

// Private hook is constructed only after the sealed claim, grant and durable
// fence are checked. The old Human writer never receives it.
type agentActivityJoinWrite struct {
	claim    *joinClaim
	effectID string
}

func eaJoinCondition(p joinApproval) ea.BoundCondition {
	return ea.BoundCondition{SourceVersion: p.b.TargetVersion, ValidUntil: p.b.ExpiresAt, Kind: ea.Join, Operation: "JOIN"}
}
func (w *agentActivityJoinWrite) record(ctx context.Context, tx pgx.Tx, p ap.Participation) error {
	n := w.claim
	if n == nil || n.g == nil || n.g.fresh(ctx, n.t) != nil {
		return aa.ErrDenied
	}
	id, e := actionUUID()
	if e != nil {
		return e
	}
	b := n.p.b
	if _, e = tx.Exec(ctx, `INSERT INTO agent_activity_join_effects(id,dispatch_id,approval_id,owner_id,agent_id,task_id,activity_id,participation_id,effect_key,state,participation_updated_at,applied_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'going',$10,clock_timestamp())`, id, n.id, b.ApprovalID, b.ActorID, b.AgentID, b.TaskID, b.TargetID, p.ID, aa.ActivityJoinEffectKey(b), p.UpdatedAt); e != nil {
		return aa.ErrUnknown
	}
	w.effectID = id
	return nil
}
func (n *joinClaim) Execute(ctx context.Context, a agentprofile.PrivateAccess) (aa.ActivityJoinReceipt, error) {
	if n == nil || n.g == nil || n.a != a || n.g.fresh(ctx, n.t) != nil || !n.used.CompareAndSwap(false, true) {
		return aa.ActivityJoinReceipt{}, aa.ErrConsumed
	}
	tx, _, e := n.g.begin(ctx, a)
	if e != nil {
		return aa.ActivityJoinReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := readJoinReceipt(ctx, tx, a, n.id, true)
	if e != nil {
		return r, e
	}
	if r.Status != aa.InFlight {
		return r, aa.ErrConsumed
	}
	var live bool
	if e = tx.QueryRow(ctx, `SELECT fence=$2 AND lease_until>clock_timestamp() FROM agent_action_dispatches WHERE id=$1`, n.id, n.fence).Scan(&live); e != nil || !live {
		return r, aa.ErrUnknown
	}
	if _, e = n.g.current(ctx, tx, a, n.p, true); e != nil {
		return r, e
	}
	if e = n.g.final(ctx, tx, n.p, n.t, false); e != nil {
		return r, e
	}
	access := joinHumanAccess(a)
	bound := eaJoinCondition(n.p)
	w := &agentActivityJoinWrite{claim: n}
	_, fresh, e := n.g.store.joinActivityTx(ctx, tx, n.p.b.ActorID, n.p.b.TargetID, &access, &bound, nil, w)
	if e != nil {
		return r, activityJoinError(e)
	}
	if !fresh || w.effectID == "" {
		return r, aa.ErrChanged
	}
	if e = n.g.final(ctx, tx, n.p, n.t, true); e != nil {
		return r, e
	}
	if e = tx.QueryRow(ctx, `SELECT fence=$2 AND lease_until>clock_timestamp() FROM agent_action_dispatches WHERE id=$1`, n.id, n.fence).Scan(&live); e != nil || !live {
		return r, aa.ErrUnknown
	}
	if e = n.g.commit(ctx, tx, n.t); e != nil {
		return aa.ActivityJoinReceipt{}, e
	}
	return n.g.ReconcileOwn(ctx, a, n.p.b.ApprovalID)
}
func (g *AgentActivityJoin) ExecuteOwn(ctx context.Context, a agentprofile.PrivateAccess, id, digest string) (aa.ActivityJoinReceipt, error) {
	r, h, e := g.CommitOwn(ctx, a, id, digest)
	if e != nil || h == nil {
		return r, e
	}
	r, claim, e := h.Begin(ctx, a)
	if e != nil {
		return r, e
	}
	return claim.Execute(ctx, a)
}
func (g *AgentActivityJoin) ReconcileOwn(ctx context.Context, a agentprofile.PrivateAccess, id string) (aa.ActivityJoinReceipt, error) {
	tx, _, e := g.beginRead(ctx, a)
	if e != nil {
		return aa.ActivityJoinReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := readJoinApproval(ctx, tx, a, id, false)
	if e != nil {
		return aa.ActivityJoinReceipt{}, e
	}
	if e = g.reader(ctx, tx, a, p.b.AgentID); e != nil {
		return aa.ActivityJoinReceipt{}, e
	}
	var did string
	if e = tx.QueryRow(ctx, `SELECT id FROM agent_action_dispatches WHERE approval_id=$1 AND owner_id=$2`, id, a.WorkspacePrincipal.ID).Scan(&did); e != nil {
		return aa.ActivityJoinReceipt{}, aa.ErrUnknown
	}
	r, e := readJoinReceipt(ctx, tx, a, did, true)
	if e != nil {
		return r, e
	}
	if r.Status != aa.Succeeded && r.Status != aa.NoEffect {
		var effect string
		var applied time.Time
		e = tx.QueryRow(ctx, `SELECT id,applied_at FROM agent_activity_join_effects WHERE dispatch_id=$1 AND approval_id=$2 AND owner_id=$3 AND agent_id=$4 AND effect_key=$5`, did, id, p.b.ActorID, p.b.AgentID, aa.ActivityJoinEffectKey(p.b)).Scan(&effect, &applied)
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE agent_action_dispatches SET state='SUCCEEDED',effect_id=$2,applied_at=$3,fence=fence+1 WHERE id=$1`, did, effect, applied)
		} else if e == pgx.ErrNoRows {
			// All possible native executors lock this original dispatch. Incrementing
			// its durable fence retires every old claim before recording absence.
			_, e = tx.Exec(ctx, `UPDATE agent_action_dispatches SET state='NO_EFFECT',fence=fence+1 WHERE id=$1`, did)
		}
		if e != nil {
			return aa.ActivityJoinReceipt{}, aa.ErrUnknown
		}
		if e = activityJoinAudit(ctx, tx, p.b.ActorID, "activity_join_reconcile", did); e != nil {
			return aa.ActivityJoinReceipt{}, e
		}
		r, e = readJoinReceipt(ctx, tx, a, did, false)
		if e != nil {
			return r, e
		}
	}
	if e = g.reader(ctx, tx, a, p.b.AgentID); e != nil {
		return r, e
	}
	r, e = g.receiptProof(ctx, tx, a, r)
	if e != nil {
		return r, e
	}
	if e = joinReadCommit(ctx, tx); e != nil {
		return aa.ActivityJoinReceipt{}, e
	}
	return r, nil
}
func (g *AgentActivityJoin) ValidateReceiptOwn(ctx context.Context, a agentprofile.PrivateAccess, r aa.ActivityJoinReceipt) error {
	proof, ok := r.Proof.(*joinProof)
	if !ok || proof == nil || proof.origin != g || proof.access != a || proof.hash != joinWire(r) || aa.ValidateActivityJoinReceipt(r, a.WorkspacePrincipal, r.ApprovalID) != nil {
		return aa.ErrDenied
	}
	tx, _, e := g.beginRead(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	cur, e := readJoinReceipt(ctx, tx, a, r.DispatchID, false)
	if e != nil {
		return e
	}
	expected := r
	expected.Proof = nil
	if !reflect.DeepEqual(cur, expected) {
		return aa.ErrChanged
	}
	var authority string
	if e = tx.QueryRow(ctx, `SELECT birdtie_activity_join_reader_authority($1,$2,$3)`, a.SessionDigest[:], a.WorkspacePrincipal.ID, r.AgentID).Scan(&authority); e != nil || authority != proof.authority {
		return aa.ErrDenied
	}
	return joinReadCommit(ctx, tx)
}

var _ json.Marshaler = (*joinClaim)(nil)
