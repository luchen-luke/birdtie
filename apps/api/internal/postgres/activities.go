package postgres

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

const activityColumns = `a.id, a.city_id, COALESCE(p.id::text, ''), COALESCE(p.name, ''), a.host_label,
    a.title, a.summary, a.starts_at, a.ends_at, a.time_zone, a.cancelled_at,
    CASE WHEN p.location_precision = 'point' AND p.coordinate_system = 'wgs84' THEN p.latitude END,
    CASE WHEN p.location_precision = 'point' AND p.coordinate_system = 'wgs84' THEN p.longitude END,
    a.source_label, a.source_ref, a.maintainer_label,
    a.updated_at, a.verified_at, a.expires_at`

const publishedActivityFrom = ` FROM activities a
    JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
    LEFT JOIN places p ON p.id = a.place_id AND p.publication_status = 'published'
      AND (p.expires_at IS NULL OR p.expires_at > now())
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

func (s *Store) SearchActivities(ctx context.Context, cityID, viewerID, category, timePreference string, closer bool) ([]foundation.Activity, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
        AND a.city_id = $1
        AND (lower(a.title || ' ' || a.summary) LIKE '%' || lower($3) || '%')
        AND a.cancelled_at IS NULL AND a.ends_at > now()
        AND (a.expires_at IS NULL OR a.expires_at > now())
        AND ($4 <> 'weekend' OR (
            a.starts_at >= ((date_trunc('week', now() AT TIME ZONE c.time_zone) + interval '5 days') AT TIME ZONE c.time_zone)
            AND a.starts_at < ((date_trunc('week', now() AT TIME ZONE c.time_zone) + interval '7 days') AT TIME ZONE c.time_zone)
        ))
        ORDER BY CASE WHEN $5 THEN
            CASE WHEN p.location_precision = 'point' AND c.map_center_latitude IS NOT NULL
                    AND c.map_center_longitude IS NOT NULL
                THEN power(p.latitude - c.map_center_latitude, 2)
                    + power((p.longitude - c.map_center_longitude) * cos(radians(c.map_center_latitude)), 2)
                ELSE 1e9 END
            ELSE 0 END ASC,
            a.starts_at, a.id LIMIT 30`, cityID, viewer, category, timePreference, closer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]foundation.Activity, 0)
	for rows.Next() {
		activity, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, activity)
	}
	return results, rows.Err()
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
	var latitude, longitude *float64
	err := row.Scan(
		&activity.ID, &activity.CityID, &activity.PlaceID, &activity.PlaceName, &activity.HostLabel,
		&activity.Title, &activity.Summary, &activity.StartsAt,
		&activity.EndsAt, &activity.TimeZone, &cancelledAt,
		&latitude, &longitude,
		&activity.Source.Label, &activity.Source.Reference,
		&activity.Source.Maintainer, &activity.Source.UpdatedAt,
		&activity.Source.VerifiedAt, &activity.Source.ExpiresAt,
	)
	if err != nil {
		return foundation.Activity{}, notFound(err)
	}
	if latitude != nil && longitude != nil {
		activity.Location = &foundation.Location{
			CoordinateSystem: "wgs84", Precision: "point",
			Latitude: latitude, Longitude: longitude,
		}
	}
	now := time.Now().UTC()
	activity.Source.SetFreshness(now)
	if location, err := time.LoadLocation(activity.TimeZone); err == nil {
		activity.Schedule = activity.StartsAt.In(location).Format("Mon · 15:04")
	}
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
