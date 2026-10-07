package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/analytics"
)

func (s *Store) RecordActivityEvent(ctx context.Context, activityID, kind, source, key string) error {
	if !analytics.ValidSource(source) {
		source = "unknown"
	}
	command, err := s.pool.Exec(ctx, `INSERT INTO activity_analytics_events
		(activity_id,event_type,entry_source,event_key)
		SELECT a.id,$2,$3,NULLIF($4,'') FROM activities a
		JOIN organizations o ON o.id=a.organization_id AND o.status='active' AND o.visibility='public'
		WHERE a.id=$1 AND a.publication_status='published' AND a.visibility='public'
		ON CONFLICT (event_key) DO NOTHING`, activityID, kind, source, key)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 && key == "" {
		return analytics.ErrNotFound
	}
	return nil
}

func (s *Store) OrganizationActivityMetrics(ctx context.Context, actorID, organizationID string) ([]analytics.ActivityMetrics, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_memberships m
		JOIN organizations o ON o.id=m.organization_id
		WHERE m.organization_id=$1 AND m.user_account_id=$2 AND m.status='active'
		AND m.role IN ('owner','admin') AND o.status='active')`, organizationID, actorID).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, analytics.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.title,COALESCE(e.entry_source,'unknown'),
		count(e.id) FILTER (WHERE e.event_type='impression'),
		count(e.id) FILTER (WHERE e.event_type='detail'),
		count(e.id) FILTER (WHERE e.event_type='rsvp'),
		count(e.id) FILTER (WHERE e.event_type='save'),
		count(e.id)
		FROM activities a LEFT JOIN activity_analytics_events e ON e.activity_id=a.id
		WHERE a.organization_id=$1 GROUP BY a.id,a.title,e.entry_source
		ORDER BY a.starts_at DESC,a.id,e.entry_source`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []analytics.ActivityMetrics{}
	indexes := map[string]int{}
	for rows.Next() {
		var id, title, source string
		var impressions, details, rsvps, saves, total int64
		if err := rows.Scan(&id, &title, &source, &impressions, &details, &rsvps, &saves, &total); err != nil {
			return nil, err
		}
		index, ok := indexes[id]
		if !ok {
			index = len(out)
			indexes[id] = index
			out = append(out, analytics.ActivityMetrics{ActivityID: id, Title: title, Sources: []analytics.SourceMetrics{}})
		}
		item := &out[index]
		item.Impressions += impressions
		item.DetailOpens += details
		item.RSVPs += rsvps
		item.Saves += saves
		if total > 0 {
			item.Sources = append(item.Sources, analytics.SourceMetrics{Source: source, Impressions: impressions, DetailOpens: details, RSVPs: rsvps, Saves: saves})
		}
	}
	return out, rows.Err()
}
