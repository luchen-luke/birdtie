package postgres

import (
	"context"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/jackc/pgx/v5"
)

// Both hooks run only in an existing native writer's ReadCommitted transaction.
// Recipients, exact City/Intent/Activity match and current visibility come from
// database facts. No API caller chooses a receiver or supplies a match score.
// A notification is not a persisted Opportunity, attendance or Agent purpose.
func routeOpportunityActivity(ctx context.Context, tx pgx.Tx, activityID string) error {
	return routeOpportunityPairs(ctx, tx, activityID, "")
}

func routeOpportunityIntentOwner(ctx context.Context, tx pgx.Tx, ownerID string) error {
	return routeOpportunityPairs(ctx, tx, "", ownerID)
}

func routeOpportunityPairs(ctx context.Context, tx pgx.Tx, activityID, ownerID string) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT a.id::text, person.id::text
 FROM activities a CROSS JOIN accounts person
 CROSS JOIN LATERAL birdtie_native_notification_source('opportunity_available',a.id,person.id) current_source
 WHERE ($1::uuid IS NULL OR a.id=$1) AND ($2::uuid IS NULL OR person.id=$2)
 AND person.account_type='person' AND person.status='active'
 AND a.publication_status='published' AND a.cancelled_at IS NULL
 ORDER BY a.id::text,person.id::text`, opportunityNotificationSelector(activityID), opportunityNotificationSelector(ownerID))
	if err != nil {
		return err
	}
	type pair struct{ activity, recipient string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err = rows.Scan(&p.activity, &p.recipient); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	// Close the result before reusing this same transaction connection. Routing
	// re-resolves the source and preference and its immutable SQL guard checks
	// them again. A source removed after collection creates no decision.
	for _, p := range pairs {
		if _, err = routeNativeNotification(ctx, tx, agentnotification.KindOpportunityAvailable, p.activity, p.recipient); err != nil {
			return err
		}
	}
	return nil
}

func opportunityNotificationSelector(id string) any {
	if id == "" {
		return nil
	}
	return id
}
