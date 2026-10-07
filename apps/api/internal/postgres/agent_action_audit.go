package postgres

import (
	"context"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/jackc/pgx/v5"
)

// Closed metadata only. IDs are actual native domain IDs, not caller text.
func actionAudit(ctx context.Context, tx pgx.Tx, actor, action, id, decision string) error {
	switch action {
	case "sandbox_preview", "sandbox_approve", "sandbox_dispatch", "sandbox_begin", "sandbox_unknown", "sandbox_applied", "sandbox_reconcile", "sandbox_no_effect", "sandbox_revoke", "sandbox_denied":
	default:
		return aa.ErrInvalid
	}
	switch decision {
	case "allowed", "denied", "error":
	default:
		return aa.ErrInvalid
	}
	_, e := auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose,target_resource_id) VALUES($1,$2,'agent_sandbox_action',$3::text,$4,'OWN_SANDBOX_ACTION',$3::text::uuid)`, actor, action, id, decision)
	return actionError(e)
}
