package postgres

import (
	"context"

	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/jackc/pgx/v5"
)

var _ aa.ApprovalRecoveryPort = (*Store)(nil)

// ReconcileOwnSandboxApproval is a receipt recovery operation, not a new
// action. Reuse the original dispatch/effect lock, permanent claim fence,
// current owner/session check and final session/controller commit boundary.
func (s *Store) ReconcileOwnSandboxApproval(ctx context.Context, a agentevent.Access, approvalID string, c *agentfeature.Controller) (aa.Dispatch, error) {
	return s.actionReadDispatchAddress(ctx, a, approvalID, c, 1, true)
}

func actionApprovalDispatchAddress(ctx context.Context, tx pgx.Tx, approvalID, owner string) (string, error) {
	if ctx == nil || !egressUUID(approvalID) || !egressUUID(owner) {
		return "", aa.ErrInvalid
	}
	if ctx.Err() != nil {
		return "", aa.ErrUnknown
	}
	var id string
	e := tx.QueryRow(ctx, `SELECT id FROM agent_action_dispatches WHERE approval_id=$1 AND owner_id=$2`, approvalID, owner).Scan(&id)
	// NoRows, transport failure and an unrecognizable receipt all remain
	// unknown. There is no fallback to Commit or a second approval.
	if e != nil || !egressUUID(id) || ctx.Err() != nil {
		return "", aa.ErrUnknown
	}
	return id, nil
}
