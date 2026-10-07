package postgres

import (
	"context"
	"encoding/json"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/jackc/pgx/v5"
	"time"
)

type nativeActionCommitment struct {
	store     *Store
	access    agentevent.Access
	id, owner string
	c         *agentfeature.Controller
	t         agentfeature.Ticket
	expires   time.Time
}

func (*nativeActionCommitment) MarshalJSON() ([]byte, error) { return nil, aa.ErrServerOnly }
func (n *nativeActionCommitment) UnmarshalJSON([]byte) error {
	*n = nativeActionCommitment{}
	return aa.ErrServerOnly
}
func actionDispatchTx(ctx context.Context, tx pgx.Tx, id, owner string, lock bool) (aa.Dispatch, error) {
	var d aa.Dispatch
	d.Schema = aa.Schema
	q := `SELECT id,approval_id,owner_id,effect_key,state,committed_at,lease_until,effect_id,applied_at FROM agent_action_dispatches WHERE id=$1 AND owner_id=$2`
	if lock {
		q += ` FOR UPDATE`
	}
	e := tx.QueryRow(ctx, q, id, owner).Scan(&d.ID, &d.ApprovalID, &d.TenantID, &d.EffectKey, &d.State, &d.CommittedAt, &d.LeaseUntil, &d.EffectID, &d.AppliedAt)
	return d, actionError(e)
}
func (s *Store) CommitOwnSandboxAction(ctx context.Context, a agentevent.Access, id string, input agenttool.SandboxProposal, c *agentfeature.Controller) (aa.Dispatch, aa.Commitment, error) {
	t, e := actionGate(ctx, c)
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if !agentplanner.ValidID(id) || !input.Valid() {
		return aa.Dispatch{}, nil, aa.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return aa.Dispatch{}, nil, aa.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Dispatch{}, nil, e
	}
	p, e := actionPreviewTx(ctx, tx, id, false)
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if _, e = s.actionCurrent(ctx, tx, a, p); e != nil {
		return aa.Dispatch{}, nil, e
	}
	// Do not expose equality of private parameters to a wrong or stale subject.
	if input != p.Proposal {
		return aa.Dispatch{}, nil, aa.ErrChanged
	}
	p, e = actionPreviewTx(ctx, tx, id, true)
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if p.State == aa.Consumed {
		var did string
		if tx.QueryRow(ctx, `SELECT id FROM agent_action_dispatches WHERE approval_id=$1`, id).Scan(&did) != nil {
			return aa.Dispatch{}, nil, aa.ErrUnknown
		}
		d, e := actionDispatchTx(ctx, tx, did, p.Binding.ActorID, false)
		return d, nil, e
	}
	if p.State != aa.Approved {
		// Only a verified native current owner reaches this denial. Record the
		// decision metadata; it creates neither approval nor an effect.
		if e = actionAudit(ctx, tx, p.Binding.ActorID, "sandbox_denied", id, "denied"); e != nil {
			return aa.Dispatch{}, nil, e
		}
		if e = egressFinish(ctx, tx, p.Binding.SessionID, nil); e != nil {
			return aa.Dispatch{}, nil, aa.ErrDenied
		}
		if e = actionCommit(ctx, tx, c, t); e != nil {
			return aa.Dispatch{}, nil, e
		}
		return aa.Dispatch{}, nil, aa.ErrApproval
	}
	var live bool
	e = tx.QueryRow(ctx, `SELECT revision=1 AND revoked_at IS NULL AND expires_at>clock_timestamp() FROM consent_grants WHERE id=$1 AND purpose='OWN_SANDBOX_ACTION' AND owner_account_id=$2 AND recipient_account_id=$2 AND resource_id=$3 AND resource_type='agent_context' AND actions=ARRAY['sandbox_write']::text[] FOR UPDATE`, p.Binding.GrantID, p.Binding.ActorID, id).Scan(&live)
	if e != nil || !live {
		return aa.Dispatch{}, nil, aa.ErrDenied
	}
	// Serialize the stable logical effect address independently of the digest.
	key := aa.EffectKey(p.Binding)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.sandbox.effect:'||$1,0))`, key); e != nil {
		return aa.Dispatch{}, nil, aa.ErrUnavailable
	}
	var existing string
	e = tx.QueryRow(ctx, `SELECT id FROM agent_action_dispatches WHERE owner_id=$1 AND effect_key=$2`, p.Binding.ActorID, key).Scan(&existing)
	if e == nil {
		d, e := actionDispatchTx(ctx, tx, existing, p.Binding.ActorID, false)
		if d.ApprovalID != id {
			return aa.Dispatch{}, nil, aa.ErrChanged
		}
		return d, nil, e
	}
	did, e := actionUUID()
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_action_approvals SET consumed_at=clock_timestamp() WHERE id=$1 AND consumed_at IS NULL`, id); e != nil {
		return aa.Dispatch{}, nil, aa.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO agent_action_dispatches(id,approval_id,owner_id,effect_key,state,committed_at) VALUES($1,$2,$3,$4,'DISPATCH_COMMITTED',clock_timestamp())`, did, id, p.Binding.ActorID, key); e != nil {
		return aa.Dispatch{}, nil, aa.ErrUnavailable
	}
	if e = actionAudit(ctx, tx, p.Binding.ActorID, "sandbox_dispatch", did, "allowed"); e != nil {
		return aa.Dispatch{}, nil, e
	}
	if e = actionFinal(ctx, tx, id, c, t); e != nil {
		return aa.Dispatch{}, nil, e
	}
	d, e := actionDispatchTx(ctx, tx, did, p.Binding.ActorID, false)
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if e = actionCommit(ctx, tx, c, t); e != nil {
		return aa.Dispatch{}, nil, e
	}
	return d, &nativeActionCommitment{s, a, did, p.Binding.ActorID, c, t, p.Binding.ExpiresAt}, nil
}
func (n *nativeActionCommitment) Begin(ctx context.Context, a agentevent.Access, c *agentfeature.Controller) (aa.Dispatch, aa.Claim, error) {
	if n == nil || n.store == nil || n.access != a || n.c != c || !c.Current(n.t) || !n.expires.After(time.Now()) || ctx == nil || ctx.Err() != nil {
		return aa.Dispatch{}, nil, aa.ErrDenied
	}
	tx, e := n.store.beginEgress(ctx)
	if e != nil {
		return aa.Dispatch{}, nil, aa.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Dispatch{}, nil, e
	}
	d, e := actionDispatchTx(ctx, tx, n.id, n.owner, true)
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if d.State != aa.Committed {
		return d, nil, aa.ErrConsumed
	}
	var fence int64
	e = tx.QueryRow(ctx, `UPDATE agent_action_dispatches SET state='IN_FLIGHT',started_at=clock_timestamp(),lease_until=LEAST($2::timestamptz,clock_timestamp()+interval '30 seconds'),fence=fence+1 WHERE id=$1 AND state='DISPATCH_COMMITTED' AND $2>clock_timestamp() RETURNING fence`, n.id, n.expires).Scan(&fence)
	if e != nil {
		return aa.Dispatch{}, nil, aa.ErrUnknown
	}
	if e = actionAudit(ctx, tx, n.owner, "sandbox_begin", n.id, "allowed"); e != nil {
		return aa.Dispatch{}, nil, e
	}
	d, e = actionDispatchTx(ctx, tx, n.id, n.owner, false)
	if e != nil {
		return aa.Dispatch{}, nil, e
	}
	if e = actionCommit(ctx, tx, c, n.t); e != nil {
		return aa.Dispatch{}, nil, e
	}
	return d, &nativeActionClaim{n.store, a, n.id, n.owner, fence, c, n.t}, nil
}

var _ json.Marshaler = (*nativeActionCommitment)(nil)
