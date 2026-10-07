package postgres

import (
	"context"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
)

type nativeActionClaim struct {
	store     *Store
	access    agentevent.Access
	id, owner string
	fence     int64
	c         *agentfeature.Controller
	t         agentfeature.Ticket
}

func (*nativeActionClaim) MarshalJSON() ([]byte, error) { return nil, aa.ErrServerOnly }
func (n *nativeActionClaim) UnmarshalJSON([]byte) error {
	*n = nativeActionClaim{}
	return aa.ErrServerOnly
}
func (n *nativeActionClaim) Execute(ctx context.Context, a agentevent.Access, c *agentfeature.Controller) (aa.Dispatch, error) {
	if n == nil || n.store == nil || ctx == nil || ctx.Err() != nil || a != n.access || c != n.c || !c.Current(n.t) {
		return aa.Dispatch{}, aa.ErrDenied
	}
	tx, e := n.store.beginEgress(ctx)
	if e != nil {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Dispatch{}, e
	}
	d, e := actionDispatchTx(ctx, tx, n.id, n.owner, true)
	if e != nil {
		return aa.Dispatch{}, e
	}
	if d.State == aa.Succeeded {
		return d, nil
	}
	if d.State != aa.InFlight {
		return d, aa.ErrUnknown
	}
	var applied bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_sandbox_writes WHERE dispatch_id=$1)`, n.id).Scan(&applied) != nil {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	if applied {
		if e = tx.Rollback(ctx); e != nil {
			return aa.Dispatch{}, aa.ErrUnknown
		}
		return n.store.ReconcileOwnSandboxDispatch(ctx, a, n.id, c)
	}
	var live bool
	e = tx.QueryRow(ctx, `SELECT fence=$2 AND lease_until>clock_timestamp() FROM agent_action_dispatches WHERE id=$1`, n.id, n.fence).Scan(&live)
	if e != nil || !live {
		return d, aa.ErrUnknown
	}
	// The earlier current grant/version check, consume and dispatch commit were
	// one sorted boundary. Post-commit revocation cannot pretend that commitment
	// never happened. This executes only its one already-committed private effect.
	p, e := actionPreviewTx(ctx, tx, d.ApprovalID, false)
	if e != nil {
		return aa.Dispatch{}, e
	}
	id, e := actionUUID()
	if e != nil {
		return aa.Dispatch{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO agent_sandbox_writes(id,dispatch_id,owner_id,target_id,effect_key,value) VALUES($1,$2,$3,$3,$4,$5)`, id, n.id, n.owner, d.EffectKey, p.Proposal.Value)
	if e != nil {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	if e = actionAudit(ctx, tx, n.owner, "sandbox_applied", n.id, "allowed"); e != nil {
		return aa.Dispatch{}, e
	}
	if e = tx.QueryRow(ctx, `SELECT state='IN_FLIGHT' AND fence=$2 AND lease_until>clock_timestamp() FROM agent_action_dispatches WHERE id=$1`, n.id, n.fence).Scan(&live); e != nil || !live || ctx.Err() != nil || !c.Current(n.t) {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	// This is a real applied row, not an invented success response. Leave the
	// delivery journal IN_FLIGHT until authoritative reconciliation acknowledges
	// it; a process dying after this commit must find this row without resending.
	if e = actionCommit(ctx, tx, c, n.t); e != nil {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	return n.store.ReconcileOwnSandboxDispatch(ctx, a, n.id, c)
}
