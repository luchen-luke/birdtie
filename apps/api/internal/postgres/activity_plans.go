package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PlanActivity(ctx context.Context, ownerID, activityID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO activity_plans (owner_account_id, activity_id)
        SELECT $1, a.id FROM activities a
        JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
        WHERE a.id = $2 AND a.publication_status = 'published'
          AND a.cancelled_at IS NULL AND a.ends_at > now()
          AND (a.expires_at IS NULL OR a.expires_at > now())
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE a.host_account_id IS NOT NULL AND
                ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                 OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))
        ON CONFLICT (owner_account_id, activity_id)
        DO UPDATE SET created_at = activity_plans.created_at
        RETURNING id`, ownerID, activityID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", activityplan.ErrNotFound
	}
	return id, err
}

func (s *Store) ListActivityPlans(ctx context.Context, ownerID string) ([]activityplan.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT pl.id, pl.activity_id,
            COALESCE(a.title, ''), COALESCE(a.city_id, ''),
            a.starts_at, a.ends_at,
            CASE WHEN a.id IS NULL THEN 'unavailable'
                 WHEN a.cancelled_at IS NOT NULL THEN 'cancelled'
                 WHEN a.ends_at <= now() THEN 'past'
                 WHEN a.starts_at <= now() THEN 'ongoing'
                 ELSE 'upcoming' END,
            a.id IS NOT NULL, pl.created_at
        FROM activity_plans pl
        LEFT JOIN activities a ON a.id = pl.activity_id
          AND a.publication_status = 'published'
          AND (a.expires_at IS NULL OR a.expires_at > now())
          AND EXISTS (SELECT 1 FROM cities c WHERE c.id = a.city_id
                      AND c.publication_status = 'published')
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE a.host_account_id IS NOT NULL AND
                ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                 OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))
        WHERE pl.owner_account_id = $1
        ORDER BY pl.created_at DESC, pl.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]activityplan.Plan, 0)
	for rows.Next() {
		var plan activityplan.Plan
		if err := rows.Scan(&plan.ID, &plan.ActivityID, &plan.Title, &plan.CityID,
			&plan.StartsAt, &plan.EndsAt, &plan.Status, &plan.Available,
			&plan.CreatedAt); err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (s *Store) RemoveActivityPlan(ctx context.Context, ownerID, id string) error {
	command, err := s.pool.Exec(ctx, `DELETE FROM activity_plans
        WHERE id = $1 AND owner_account_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return activityplan.ErrNotFound
	}
	return nil
}
