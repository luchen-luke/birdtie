package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PlanActivity(ctx context.Context, ownerID, activityID string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var id string
	var created bool
	err = tx.QueryRow(ctx, `INSERT INTO activity_plans (owner_account_id, activity_id)
        SELECT $1, a.id FROM activities a
        JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
        WHERE a.id = $2 AND a.publication_status = 'published'
          AND birdtie_activity_visible_to(a.id, $1)
          AND (c.expires_at IS NULL OR c.expires_at > clock_timestamp())
          AND a.cancelled_at IS NULL AND a.ends_at > clock_timestamp()
          AND (a.expires_at IS NULL OR a.expires_at > clock_timestamp())
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE a.host_account_id IS NOT NULL AND
                ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                 OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))
        ON CONFLICT (owner_account_id, activity_id)
        DO UPDATE SET created_at = activity_plans.created_at
        RETURNING id,(xmax=0)`, ownerID, activityID).Scan(&id, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", activityplan.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if created {
		if err = insertDomainAudit(ctx, tx, ownerID, "create", "activity_plan", id, "human_private_reminder", &activityID); err != nil {
			return "", err
		}
	}
	var current bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activities a JOIN cities c ON c.id=a.city_id AND c.publication_status='published'
 WHERE a.id=$2 AND a.publication_status='published' AND birdtie_activity_visible_to(a.id,$1)
 AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp()
 AND(a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND NOT EXISTS(SELECT 1 FROM account_blocks b
 WHERE a.host_account_id IS NOT NULL AND ((b.blocker_account_id=$1 AND b.blocked_account_id=a.host_account_id)
 OR(b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$1))))`, ownerID, activityID).Scan(&current); err != nil {
		return "", err
	}
	if !current {
		return "", activityplan.ErrNotFound
	}
	return id, tx.Commit(ctx)
}

func (s *Store) ListActivityPlans(ctx context.Context, ownerID string) ([]activityplan.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT pl.id, pl.activity_id,
            COALESCE(a.title, ''), COALESCE(a.city_id, ''),
            a.starts_at, a.ends_at,
            CASE WHEN a.id IS NULL THEN 'unavailable'
                 WHEN a.cancelled_at IS NOT NULL THEN 'cancelled'
                 WHEN a.ends_at <= clock_timestamp() THEN 'past'
                 WHEN a.starts_at <= clock_timestamp() THEN 'ongoing'
                 ELSE 'upcoming' END,
            a.id IS NOT NULL, pl.created_at,
            COALESCE(a.modality,''), COALESCE(a.physical_place_status,''),
            COALESCE(loc.id::text,''), COALESCE(loc.name,''),
            CASE WHEN venue.place_id IS NOT NULL THEN venue.place_id::text ELSE '' END, COALESCE(a.time_zone,'')
        FROM activity_plans pl
        LEFT JOIN activities a ON a.id = pl.activity_id
          AND a.publication_status = 'published'
          AND birdtie_activity_visible_to(a.id, $1)
          AND (a.expires_at IS NULL OR a.expires_at > clock_timestamp())
          AND EXISTS (SELECT 1 FROM cities c WHERE c.id = a.city_id
                      AND c.publication_status = 'published'
                      AND (c.expires_at IS NULL OR c.expires_at > clock_timestamp()))
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE a.host_account_id IS NOT NULL AND
                ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                 OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))
        LEFT JOIN places loc ON loc.id=a.place_id AND loc.city_id=a.city_id
          AND loc.publication_status='published' AND (loc.expires_at IS NULL OR loc.expires_at>clock_timestamp())
        LEFT JOIN venues venue ON venue.place_id=a.venue_place_id AND venue.place_id=loc.id AND venue.city_id=a.city_id
          AND venue.expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM venue_candidates vc WHERE vc.id=venue.source_candidate_id
              AND vc.status='approved' AND vc.place_id=venue.place_id AND vc.city_id=venue.city_id
              AND vc.reviewed_by=venue.reviewed_by AND vc.expires_at>clock_timestamp())
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
			&plan.CreatedAt, &plan.Modality, &plan.PhysicalPlaceStatus, &plan.PlaceID, &plan.PlaceName, &plan.VenuePlaceID, &plan.TimeZone); err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (s *Store) RemoveActivityPlan(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var target string
	err = tx.QueryRow(ctx, `DELETE FROM activity_plans WHERE id=$1 AND owner_account_id=$2 RETURNING activity_id`, id, ownerID).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return activityplan.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err = insertDomainAudit(ctx, tx, ownerID, "delete", "activity_plan", id, "human_private_reminder", &target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
