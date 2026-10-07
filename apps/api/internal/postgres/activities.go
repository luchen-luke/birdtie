package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// Only this server-generated Person origin copied a UserProfile name. Other
// sources supply independent activity labels and keep their resource semantics.
// Eligibility belongs in the projection, not the origin predicate: a suspended
// or removed Agent must never turn its old copied name into an independent label.
const nativePersonActivityLabelOrigin = `ao.person_account_id IS NOT NULL
    AND ao.person_account_id=a.host_account_id
    AND ao.person_account_id=a.created_by_account_id
    AND a.source_label='Birdtie member'
    AND a.source_ref ~ '^birdtie:activity:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'`

// viewerSQL and independentLabelSQL are fixed, internal SQL expressions, never
// caller text. The label and field policy are resolved by the payload statement
// for its actual reader; neither a historical copy nor another reader is reused.
func projectedActivityLabelSQL(viewerSQL, independentLabelSQL string) string {
	return `CASE WHEN ` + nativePersonActivityLabelOrigin + ` THEN
      COALESCE((SELECT CASE
          WHEN birdtie_agent_profile_field_allowed(label_owner.id,` + viewerSQL + `::uuid,'displayName')
            AND (label_profile.visibility='public' OR label_owner.id=` + viewerSQL + `::uuid
              OR EXISTS(SELECT 1 FROM consent_grants label_grant
                WHERE label_grant.owner_account_id=label_owner.id
                  AND label_grant.recipient_account_id=` + viewerSQL + `::uuid
                  AND label_grant.resource_type='profile'
                  AND label_grant.resource_id=label_owner.id::text
                  AND label_grant.purpose='profile_view'
                  AND label_grant.actions @> ARRAY['read']::text[]
                  AND label_grant.revoked_at IS NULL
                  AND (label_grant.expires_at IS NULL OR label_grant.expires_at>statement_timestamp())))
            THEN COALESCE(NULLIF(label_profile.display_name,''),NULLIF(label_owner.handle,''),'Birdtie 成员')
          ELSE 'Birdtie 成员' END
        FROM accounts label_owner
        LEFT JOIN user_profiles label_profile ON label_profile.account_id=label_owner.id
        WHERE label_owner.id=ao.person_account_id AND label_owner.account_type='person'), 'Birdtie 成员')
      ELSE ` + independentLabelSQL + ` END`
}

// This is the persisted source-review provenance, not a current candidate or
// reviewer permission. Only the automatically copied maintenance label is
// generalized; an independently supplied event host remains a separate fact.
const reviewedActivityMaintainerSQL = `CASE WHEN EXISTS(
    SELECT 1 FROM activity_sources maintainer_origin
    WHERE maintainer_origin.activity_id=a.id
      AND maintainer_origin.source_url=a.source_ref
      AND maintainer_origin.source_label=a.source_label
      AND maintainer_origin.reviewer_account_id IS NOT NULL)
    THEN '城市维护者' ELSE a.maintainer_label END`

var activityColumns = `a.id, a.city_id, COALESCE(p.id::text, ''), COALESCE(p.name, ''), ` + projectedActivityLabelSQL("$2", "a.host_label") + `,
    a.title, a.summary, a.description, COALESCE(a.category_code, ''), a.capacity,
    CASE WHEN a.capacity IS NOT NULL THEN
      (SELECT count(*)::integer FROM activity_participations ap
       WHERE ap.activity_id=a.id AND ap.status IN ('going','pending')) END,
    a.price_minor, COALESCE(a.currency, ''), a.eligibility, COALESCE(a.language_code, ''),
    COALESCE((SELECT link FROM jsonb_array_elements_text(o.official_links) AS links(link)
      WHERE link LIKE 'https://%' LIMIT 1), ''),
    a.starts_at, a.ends_at, a.time_zone, a.cancelled_at,
    CASE WHEN p.location_precision = 'point' AND p.coordinate_system = 'wgs84' THEN p.latitude END,
    CASE WHEN p.location_precision = 'point' AND p.coordinate_system = 'wgs84' THEN p.longitude END,
    a.source_label, a.source_ref, ` + projectedActivityLabelSQL("$2", reviewedActivityMaintainerSQL) + `,
    a.updated_at, a.verified_at, a.expires_at, o.id, a.visibility,
    CASE WHEN ao.person_account_id IS NOT NULL THEN 'PERSON'
         WHEN ao.community_id IS NOT NULL THEN 'COMMUNITY'
         WHEN ao.business_id IS NOT NULL THEN 'BUSINESS' ELSE 'ORGANIZATION' END,
    COALESCE(ao.person_account_id::text,ao.community_id::text,ao.organization_id::text,ao.business_id::text),
    CASE WHEN ao.person_account_id IS NOT NULL THEN ` + projectedActivityLabelSQL("$2", "a.host_label") + `
         WHEN ao.community_id IS NOT NULL THEN cc.name
         WHEN ao.business_id IS NOT NULL THEN bb.name ELSE oo.name END,
    cc.avatar_url, a.modality, a.physical_place_status,
    CASE WHEN v.place_id IS NOT NULL THEN v.place_id::text END`

const publishedActivityFrom = ` FROM activities a
    JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
    LEFT JOIN places p ON p.id = a.place_id AND p.publication_status = 'published'
      AND (p.expires_at IS NULL OR p.expires_at > now())
    LEFT JOIN organizations o ON o.id=a.organization_id AND o.visibility='public'
      AND o.status='active'
    JOIN activity_organizers ao ON ao.activity_id=a.id
    LEFT JOIN communities cc ON cc.id=ao.community_id
    LEFT JOIN organizations oo ON oo.id=ao.organization_id
    LEFT JOIN businesses bb ON bb.id=ao.business_id
    LEFT JOIN venues v ON v.place_id=a.venue_place_id AND v.place_id=p.id
      AND v.expires_at>now()
      AND EXISTS(SELECT 1 FROM venue_candidates vc WHERE vc.id=v.source_candidate_id
        AND vc.status='approved' AND vc.reviewed_by=v.reviewed_by)
    WHERE a.publication_status = 'published'
      AND birdtie_activity_visible_to(a.id,$2::uuid)
      AND NOT EXISTS (
        SELECT 1 FROM account_blocks b
        WHERE $2::uuid IS NOT NULL AND a.host_account_id IS NOT NULL
          AND ((b.blocker_account_id = $2 AND b.blocked_account_id = a.host_account_id)
            OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $2))
      )`

const activityDiscoveryWhere = `
        AND a.city_id = $1
        AND a.cancelled_at IS NULL AND a.ends_at > now()
        AND (a.expires_at IS NULL OR a.expires_at > now())
        AND ($3::double precision IS NULL OR (
            p.location_precision = 'point' AND p.coordinate_system = 'wgs84'
            AND p.longitude BETWEEN $3 AND $5
            AND p.latitude BETWEEN $4 AND $6
        ))
        AND ($7::timestamptz IS NULL OR a.ends_at > $7)
        AND ($8::timestamptz IS NULL OR a.starts_at < $8)
        AND ($9::text = '' OR a.category_code = $9)`

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

func (s *Store) ListPlaceActivities(ctx context.Context, placeID, viewerID string) ([]foundation.Activity, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
		AND a.place_id=$1 AND a.cancelled_at IS NULL AND a.ends_at>now()
		AND (a.expires_at IS NULL OR a.expires_at>now())
		ORDER BY a.starts_at,a.id LIMIT 100`, placeID, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []foundation.Activity{}
	for rows.Next() {
		item, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// FindActivities returns only current, public activities whose schedules
// overlap the requested interval. A bounded query requires a published point
// location, so activities without precise coordinates are excluded from maps.
func (s *Store) FindActivities(ctx context.Context, cityID, viewerID string, filter foundation.ActivitySearchFilter) ([]foundation.Activity, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+activityDiscoveryWhere+`
        ORDER BY a.starts_at, a.id LIMIT 100`, cityID, viewer,
		filter.West, filter.South, filter.East, filter.North,
		filter.From, filter.To, filter.Category)
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

// AreaPulse aggregates the same eligible rows as public Activity discovery.
// The rollup row gives the exact total; the two largest actual category codes
// provide optional facets without fabricating labels or counts.
func (s *Store) AreaPulse(ctx context.Context, cityID, viewerID string, filter foundation.ActivitySearchFilter) (foundation.AreaPulse, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := s.pool.Query(ctx, `SELECT GROUPING(a.category_code), COALESCE(a.category_code, ''), count(*)
        `+publishedActivityFrom+activityDiscoveryWhere+`
        GROUP BY ROLLUP(a.category_code)
        ORDER BY GROUPING(a.category_code) DESC, count(*) DESC, a.category_code
        LIMIT 3`, cityID, viewer,
		filter.West, filter.South, filter.East, filter.North,
		filter.From, filter.To, "")
	if err != nil {
		return foundation.AreaPulse{}, err
	}
	defer rows.Close()
	pulse := foundation.AreaPulse{CityID: cityID, Status: "empty", Categories: []foundation.PulseCategory{}}
	for rows.Next() {
		var rollup int
		var code string
		var count int64
		if err := rows.Scan(&rollup, &code, &count); err != nil {
			return foundation.AreaPulse{}, err
		}
		if rollup == 1 {
			pulse.Total = count
		} else if code != "" {
			pulse.Categories = append(pulse.Categories, foundation.PulseCategory{Code: code, Count: count})
		}
	}
	if err := rows.Err(); err != nil {
		return foundation.AreaPulse{}, err
	}
	if pulse.Total > 0 {
		pulse.Status = "populated"
	}
	return pulse, nil
}

func (s *Store) SearchActivities(ctx context.Context, cityID, viewerID, category, timePreference string, closer bool, bounds *agentworkspace.MapBounds) ([]foundation.Activity, error) {
	var viewer any
	var west, south, east, north any
	if viewerID != "" {
		viewer = viewerID
	}
	if bounds != nil {
		west, south, east, north = bounds.West, bounds.South, bounds.East, bounds.North
	}
	rows, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
        AND a.city_id = $1
        AND (a.category_code = $3 OR lower(a.title || ' ' || a.summary) LIKE '%' || lower($3) || '%')
        AND a.cancelled_at IS NULL AND a.ends_at > now()
        AND (a.expires_at IS NULL OR a.expires_at > now())
        AND ($4 <> 'weekend' OR (
            a.starts_at >= ((date_trunc('week', now() AT TIME ZONE c.time_zone) + interval '5 days') AT TIME ZONE c.time_zone)
            AND a.starts_at < ((date_trunc('week', now() AT TIME ZONE c.time_zone) + interval '7 days') AT TIME ZONE c.time_zone)
        ))
		AND ($4 <> 'today' OR (a.starts_at AT TIME ZONE c.time_zone)::date = (now() AT TIME ZONE c.time_zone)::date)
		AND ($4 <> 'tomorrow' OR (a.starts_at AT TIME ZONE c.time_zone)::date = ((now() AT TIME ZONE c.time_zone)::date + 1))
		AND ($4 <> 'tonight' OR (
			(a.starts_at AT TIME ZONE c.time_zone)::date = (now() AT TIME ZONE c.time_zone)::date
			AND (a.starts_at AT TIME ZONE c.time_zone)::time >= time '18:00'
		))
        AND ($6::double precision IS NULL OR (
            p.location_precision = 'point' AND p.coordinate_system = 'wgs84'
            AND p.longitude BETWEEN $6 AND $8
            AND p.latitude BETWEEN $7 AND $9
        ))
        ORDER BY CASE WHEN $5 THEN
            CASE WHEN p.location_precision = 'point' AND c.map_center_latitude IS NOT NULL
                    AND c.map_center_longitude IS NOT NULL
                THEN power(p.latitude - c.map_center_latitude, 2)
                    + power((p.longitude - c.map_center_longitude) * cos(radians(c.map_center_latitude)), 2)
                ELSE 1e9 END
            ELSE 0 END ASC,
            a.starts_at, a.id LIMIT 30`, cityID, viewer, category, timePreference, closer, west, south, east, north)
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
		&activity.Title, &activity.Summary, &activity.Description, &activity.CategoryCode,
		&activity.Capacity, &activity.ParticipantCount, &activity.PriceMinor,
		&activity.Currency, &activity.Eligibility, &activity.LanguageCode,
		&activity.OfficialURL, &activity.StartsAt,
		&activity.EndsAt, &activity.TimeZone, &cancelledAt,
		&latitude, &longitude,
		&activity.Source.Label, &activity.Source.Reference,
		&activity.Source.Maintainer, &activity.Source.UpdatedAt,
		&activity.Source.VerifiedAt, &activity.Source.ExpiresAt,
		&activity.OrganizationID, &activity.Visibility,
		&activity.Organizer.Type, &activity.Organizer.ID, &activity.Organizer.Name,
		&activity.Organizer.AvatarURL,
		&activity.Modality, &activity.PhysicalPlaceStatus, &activity.VenuePlaceID,
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
		activity.Schedule = formatActivitySchedule(activity.StartsAt.In(location))
		activity.EndSchedule = formatActivitySchedule(activity.EndsAt.In(location))
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

func formatActivitySchedule(local time.Time) string {
	weekdays := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return fmt.Sprintf("%d月%d日（%s）%02d:%02d",
		local.Month(), local.Day(), weekdays[local.Weekday()], local.Hour(), local.Minute())
}
