package postgres

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

const activityColumns = `a.id, a.city_id, COALESCE(p.id::text, ''), a.host_label,
    a.title, a.summary, a.starts_at, a.ends_at, a.time_zone, a.cancelled_at,
    a.source_label, a.source_ref, a.maintainer_label,
    a.updated_at, a.verified_at, a.expires_at`

const publishedActivityFrom = ` FROM activities a
    JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
    LEFT JOIN places p ON p.id = a.place_id AND p.publication_status = 'published'
    WHERE a.publication_status = 'published'
      AND NOT EXISTS (
        SELECT 1 FROM account_blocks b
        WHERE $2::uuid IS NOT NULL AND a.host_account_id IS NOT NULL
          AND ((b.blocker_account_id = $2 AND b.blocked_account_id = a.host_account_id)
            OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $2))
      )`

func (s *Store) ListActivities(ctx context.Context, cityID, viewerID string) ([]foundation.Activity, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
        AND a.city_id = $1
        ORDER BY (a.ends_at <= now() OR a.cancelled_at IS NOT NULL),
                 abs(extract(epoch from (a.starts_at - now()))), a.id
        LIMIT 100`, cityID, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	activities := make([]foundation.Activity, 0)
	for rows.Next() {
		activity, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		activities = append(activities, activity)
	}
	return activities, rows.Err()
}

func (s *Store) GetActivity(ctx context.Context, id, viewerID string) (foundation.Activity, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	row := s.pool.QueryRow(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
        AND a.id = $1`, id, viewer)
	return scanActivity(row)
}

func scanActivity(row scanner) (foundation.Activity, error) {
	var activity foundation.Activity
	var cancelledAt *time.Time
	err := row.Scan(
		&activity.ID, &activity.CityID, &activity.PlaceID, &activity.HostLabel,
		&activity.Title, &activity.Summary, &activity.StartsAt,
		&activity.EndsAt, &activity.TimeZone, &cancelledAt,
		&activity.Source.Label, &activity.Source.Reference,
		&activity.Source.Maintainer, &activity.Source.UpdatedAt,
		&activity.Source.VerifiedAt, &activity.Source.ExpiresAt,
	)
	if err != nil {
		return foundation.Activity{}, notFound(err)
	}
	now := time.Now().UTC()
	activity.Source.SetFreshness(now)
	switch {
	case cancelledAt != nil:
		activity.Status = "cancelled"
	case !activity.EndsAt.After(now):
		activity.Status = "past"
	case !activity.StartsAt.After(now):
		activity.Status = "ongoing"
	default:
		activity.Status = "upcoming"
	}
	return activity, nil
}
