package postgres

import (
	"context"
	"errors"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) ReadOwnSandboxDispatch(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (aa.Dispatch, error) {
	return s.actionReadDispatch(ctx, a, id, c, 0)
}
func (s *Store) ReconcileOwnSandboxDispatch(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (aa.Dispatch, error) {
	return s.actionReadDispatch(ctx, a, id, c, 1)
}
func (s *Store) MarkOwnSandboxUnknown(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (aa.Dispatch, error) {
	return s.actionReadDispatch(ctx, a, id, c, 2)
}
func (s *Store) actionReadDispatch(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller, mode int) (aa.Dispatch, error) {
	return s.actionReadDispatchAddress(ctx, a, id, c, mode, false)
}

func (s *Store) actionReadDispatchAddress(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller, mode int, byApproval bool) (aa.Dispatch, error) {
	t, e := actionGate(ctx, c)
	if e != nil {
		return aa.Dispatch{}, e
	}
	if !egressUUID(id) {
		return aa.Dispatch{}, aa.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Dispatch{}, e
	}
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return aa.Dispatch{}, aa.ErrDenied
	}
	if byApproval {
		// Resolve only an already committed receipt for this authenticated owner
		// inside the original transaction. Missing/failed lookup is UNKNOWN,
		// never permission to dispatch or proof that no effect occurred.
		id, e = actionApprovalDispatchAddress(ctx, tx, id, owner)
		if e != nil {
			return aa.Dispatch{}, e
		}
	}
	d, e := actionDispatchTx(ctx, tx, id, owner, mode != 0)
	if e != nil {
		return aa.Dispatch{}, e
	}
	if mode == 2 && d.State != aa.Succeeded && d.State != aa.NoEffect && d.State != aa.Unknown {
		if _, e = tx.Exec(ctx, `UPDATE agent_action_dispatches SET state='UNKNOWN_OUTCOME',fence=fence+1 WHERE id=$1`, id); e != nil {
			return aa.Dispatch{}, aa.ErrUnknown
		}
		if e = actionAudit(ctx, tx, owner, "sandbox_unknown", id, "error"); e != nil {
			return aa.Dispatch{}, e
		}
		d, e = actionDispatchTx(ctx, tx, id, owner, false)
		if e != nil {
			return aa.Dispatch{}, e
		}
	}
	if mode == 1 && d.State != aa.Succeeded && d.State != aa.NoEffect {
		var effect string
		var at time.Time
		e = tx.QueryRow(ctx, `SELECT id,applied_at FROM agent_sandbox_writes WHERE dispatch_id=$1 AND owner_id=$2 AND effect_key=$3`, id, owner, d.EffectKey).Scan(&effect, &at)
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE agent_action_dispatches SET state='SUCCEEDED',effect_id=$2,applied_at=$3,fence=fence+1 WHERE id=$1`, id, effect, at)
			if e != nil {
				return aa.Dispatch{}, aa.ErrUnknown
			}
			if e = actionAudit(ctx, tx, owner, "sandbox_reconcile", id, "allowed"); e != nil {
				return aa.Dispatch{}, e
			}
		} else if errors.Is(e, pgx.ErrNoRows) {
			// Native sandbox is the only executor and MUST lock this row before its
			// actual write. Holding the row and incrementing its fence closes every old
			// claim. Absence is authoritative only together with that permanent fence;
			// it is never interpreted as a reason to send/retry this operation again.
			if _, e = tx.Exec(ctx, `UPDATE agent_action_dispatches SET state='NO_EFFECT',fence=fence+1 WHERE id=$1`, id); e != nil {
				return aa.Dispatch{}, aa.ErrUnknown
			}
			if e = actionAudit(ctx, tx, owner, "sandbox_no_effect", id, "allowed"); e != nil {
				return aa.Dispatch{}, e
			}
		} else {
			return aa.Dispatch{}, aa.ErrUnknown
		}
		d, e = actionDispatchTx(ctx, tx, id, owner, false)
		if e != nil {
			return aa.Dispatch{}, e
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return aa.Dispatch{}, aa.ErrDenied
	}
	if e = actionCommit(ctx, tx, c, t); e != nil {
		return aa.Dispatch{}, aa.ErrUnknown
	}
	return d, nil
}
