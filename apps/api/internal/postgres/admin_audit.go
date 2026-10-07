package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func insertAdminAudit(ctx context.Context, tx pgx.Tx, actorID, organizationID, resourceType, resourceID, action string) error {
	_, err := auditExec(ctx, tx, `INSERT INTO admin_audit_events
		(actor_account_id,organization_id,resource_type,resource_id,action)
		VALUES ($1,$2,$3,$4,$5)`, actorID, organizationID, resourceType, resourceID, action)
	return err
}
