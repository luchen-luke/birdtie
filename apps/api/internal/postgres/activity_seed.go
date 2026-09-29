package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/jackc/pgx/v5"
)

const activityCandidateColumns = `id, city_id, submitted_by,
    COALESCE(place_id::text, ''), title, summary, host_label,
    starts_at, ends_at, time_zone, source_label, source_url, rights_note,
    expires_at, status, COALESCE(reviewed_by::text, ''), reviewed_at,
    COALESCE(review_note, ''), COALESCE(resolved_activity_id::text, ''), created_at`

func scanActivityCandidate(row scanner) (cityseed.ActivityCandidate, error) {
	var c cityseed.ActivityCandidate
	err := row.Scan(&c.ID, &c.CityID, &c.SubmittedBy, &c.PlaceID,
		&c.Title, &c.Summary, &c.HostLabel, &c.StartsAt, &c.EndsAt,
		&c.TimeZone, &c.SourceLabel, &c.SourceURL, &c.RightsNote,
		&c.ExpiresAt, &c.Status, &c.ReviewedBy, &c.ReviewedAt,
		&c.ReviewNote, &c.ResolvedActivityID, &c.CreatedAt)
	return c, err
}

func (s *Store) SubmitActivity(ctx context.Context, actorID, cityID string, input cityseed.ActivityInput) (cityseed.ActivityCandidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	defer tx.Rollback(ctx)
	if input.PlaceID != "" {
		var placeID string
		err = tx.QueryRow(ctx, `SELECT id FROM places
            WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
            FOR SHARE`, input.PlaceID, cityID).Scan(&placeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cityseed.ActivityCandidate{}, cityseed.ErrConflict
		}
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
	}
	c, err := scanActivityCandidate(tx.QueryRow(ctx, `INSERT INTO activity_candidates (
        city_id, submitted_by, place_id, title, summary, host_label,
        starts_at, ends_at, time_zone, source_label, source_url, rights_note,
        expires_at
    )
    SELECT c.id, a.id, NULLIF($3, '')::uuid, $4, $5, $6,
           $7, $8, $9, $10, $11, $12, $13
    FROM city_editor_memberships m
    JOIN cities c ON c.id = m.city_id AND c.publication_status = 'published'
    JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
    WHERE m.city_id = $1 AND m.account_id = $2 AND m.state = 'active'
    RETURNING `+activityCandidateColumns,
		cityID, actorID, input.PlaceID, input.Title, input.Summary,
		input.HostLabel, input.StartsAt, input.EndsAt, input.TimeZone,
		input.SourceLabel, input.SourceURL, input.RightsNote, input.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.ActivityCandidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'submit', 'activity_candidate', $2, 'allowed', 'city_seed')`,
		actorID, c.ID)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	return c, nil
}

func (s *Store) ListActivityCandidates(ctx context.Context, actorID, cityID string) ([]cityseed.ActivityCandidate, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM city_editor_memberships m
        JOIN accounts a ON a.id = m.account_id
        WHERE m.city_id = $1 AND m.account_id = $2
          AND m.state = 'active' AND a.status = 'active'
    )`, cityID, actorID).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, cityseed.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT `+activityCandidateColumns+`
        FROM activity_candidates WHERE city_id = $1 AND status = 'pending'
        ORDER BY created_at, id LIMIT 100`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]cityseed.ActivityCandidate, 0)
	for rows.Next() {
		c, err := scanActivityCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (s *Store) ReviewActivity(ctx context.Context, actorID, candidateID string, input cityseed.ActivityReviewInput) (cityseed.ActivityCandidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanActivityCandidate(tx.QueryRow(ctx, `SELECT `+activityCandidateColumns+`
        FROM activity_candidates WHERE id = $1 AND city_id IN (
            SELECT m.city_id FROM city_editor_memberships m
            JOIN accounts a ON a.id = m.account_id
            WHERE m.account_id = $2 AND m.role = 'reviewer'
              AND m.state = 'active' AND a.status = 'active'
        ) FOR UPDATE`, candidateID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.ActivityCandidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if c.SubmittedBy == actorID || c.Status != "pending" {
		return cityseed.ActivityCandidate{}, cityseed.ErrConflict
	}
	if input.Decision == "publish" && !c.ExpiresAt.After(time.Now()) {
		return cityseed.ActivityCandidate{}, cityseed.ErrConflict
	}
	var activityID any
	if input.Decision == "publish" {
		if c.PlaceID != "" {
			var placeID string
			err = tx.QueryRow(ctx, `SELECT id FROM places
                WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
                FOR SHARE`, c.PlaceID, c.CityID).Scan(&placeID)
			if errors.Is(err, pgx.ErrNoRows) {
				return cityseed.ActivityCandidate{}, cityseed.ErrConflict
			}
			if err != nil {
				return cityseed.ActivityCandidate{}, err
			}
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO activities (
            city_id, place_id, title, summary, host_label, starts_at, ends_at,
            time_zone, publication_status, source_label, source_ref,
            maintainer_label, verified_at, expires_at
        )
        SELECT $1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7,
               $8, 'published', $9, $10,
               COALESCE(NULLIF(a.handle, ''), 'Birdtie city editor'),
               now(), $11
        FROM accounts a WHERE a.id = $12 AND a.status = 'active'
        RETURNING id`, c.CityID, c.PlaceID, c.Title, c.Summary,
			c.HostLabel, c.StartsAt, c.EndsAt, c.TimeZone,
			c.SourceLabel, c.SourceURL, c.ExpiresAt, actorID).Scan(&id)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
		activityID = id
		_, err = tx.Exec(ctx, `INSERT INTO activity_sources (
            activity_id, candidate_id, source_label, source_url, rights_note,
            reviewer_account_id, verified_at, expires_at
        ) VALUES ($1, $2, $3, $4, $5, $6, now(), $7)`,
			id, c.ID, c.SourceLabel, c.SourceURL, c.RightsNote,
			actorID, c.ExpiresAt)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO city_seed_activities (
            city_id, activity_id, maintainer_account_id, valid_until
        ) VALUES ($1, $2, $3, $4)`, c.CityID, id, actorID, c.ExpiresAt)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
	} else if input.Decision != "reject" {
		return cityseed.ActivityCandidate{}, cityseed.ErrConflict
	}
	status := "rejected"
	if activityID != nil {
		status = "published"
	}
	c, err = scanActivityCandidate(tx.QueryRow(ctx, `UPDATE activity_candidates
        SET status = $2, reviewed_by = $3, reviewed_at = now(),
            review_note = $4, resolved_activity_id = $5
        WHERE id = $1 AND status = 'pending'
        RETURNING `+activityCandidateColumns,
		candidateID, status, actorID, input.Note, activityID))
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'activity_candidate', $3, 'allowed', 'city_seed')`,
		actorID, status, candidateID)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	detail := "Your activity suggestion was not published after review."
	if status == "published" {
		detail = "Your activity suggestion is now visible in Birdtie's public city results."
	}
	if err := insertReviewInboxItem(ctx, tx, c.SubmittedBy, "activity_candidate", c.ID,
		"Activity suggestion reviewed", detail); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	return c, nil
}
