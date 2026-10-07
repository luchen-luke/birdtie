package postgres

import (
	"context"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/jackc/pgx/v5"
)

// Existing072 publication still works before106. An unavailable producer is
// never replaced by legacy/untyped Inbox or fabricated source data.
func prepareBusinessPublicUpdate(ctx context.Context, tx pgx.Tx) (bool, error) {
	var enabled bool
	if e := tx.QueryRow(ctx, `SELECT to_regprocedure('birdtie_native_business_public_update(uuid,uuid)') IS NOT NULL`).Scan(&enabled); e != nil {
		return false, e
	}
	if !enabled {
		return false, nil
	}
	// businessBegin has already locked the acting Account FOR SHARE. Reserve
	// decision writes BEFORE permission/audit writes: scheduler takes its owner
	// Account NO KEY UPDATE, then decision/source SHARE. Recipient FK KEY SHARE
	// is compatible with its NO KEY UPDATE; do not take recipient FOR SHARE here.
	_, e := tx.Exec(ctx, `LOCK TABLE native_notification_decisions IN ROW EXCLUSIVE MODE`)
	return enabled, e
}

// Called only from a genuinely new explicit publication, in its original Tx.
// The audit ID and current SQL, never human/Agent labels, select recipients.
func routeBusinessPublicUpdate(ctx context.Context, tx pgx.Tx, auditID string) error {
	rows, e := tx.Query(ctx, `SELECT follow.follower_account_id FROM follows follow
 JOIN business_public_profile_audit audit ON audit.business_id=follow.business_id
 WHERE audit.id=$1 AND EXISTS(SELECT 1 FROM birdtie_native_business_public_update(audit.id,follow.follower_account_id))
 ORDER BY follow.follower_account_id`, auditID)
	if e != nil {
		return e
	}
	var recipients []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		recipients = append(recipients, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, recipient := range recipients {
		_, e = routeNativeNotification(ctx, tx, agentnotification.KindBusinessUpdate, auditID, recipient)
		if e != nil {
			return e
		}
	}
	return nil
}
