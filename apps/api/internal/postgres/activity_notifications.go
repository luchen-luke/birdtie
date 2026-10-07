package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"

	"github.com/jackc/pgx/v5"
)

// A distinct event UUID lets a participant receive multiple genuine edits to
// the same Activity while the inbox's existing owner/event uniqueness remains.
func insertActivityChangeInbox(ctx context.Context, tx pgx.Tx, activityID, kind, _title, _detail string) error {
	rows, err := tx.Query(ctx, `SELECT participant_account_id FROM activity_participations
	 WHERE activity_id=$1 AND status IN ('going','pending') ORDER BY participant_account_id`, activityID)
	if err != nil {
		return err
	}
	var recipients []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range recipients {
		if _, err = routeNativeNotification(ctx, tx, agentnotification.Kind(kind), activityID, id); err != nil {
			return err
		}
	}
	return nil
}

// The API runs this at startup and periodically. The inbox uniqueness key
// makes the reminder idempotent across retries, processes and restarts.
func (s *Store) EnqueueStartsSoonReminders(ctx context.Context) (int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT a.id,p.participant_account_id
		FROM activity_participations p
		JOIN activities a ON a.id=p.activity_id
		JOIN cities c ON c.id=a.city_id AND c.publication_status='published'
		WHERE p.status IN ('going','pending')
		  AND a.publication_status='published' AND a.visibility='public'
		  AND a.cancelled_at IS NULL
		  AND a.starts_at > now() AND a.starts_at <= now()+interval '2 hours'
		  AND a.ends_at > now()
		ORDER BY a.id,p.participant_account_id FOR UPDATE OF a,p`)
	if err != nil {
		return 0, err
	}
	var pairs [][2]string
	for rows.Next() {
		var pair [2]string
		if err = rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, pair)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	var inserted int64
	for _, pair := range pairs {
		var delivered bool
		delivered, err = routeNativeNotification(ctx, tx, agentnotification.KindActivityReminder, pair[0], pair[1])
		if err != nil {
			return 0, err
		}
		if delivered {
			inserted++
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return inserted, nil
}
