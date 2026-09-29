package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/jackc/pgx/v5"
)

const communityColumns = `id, city_id, owner_account_id,
    COALESCE(place_id::text, ''), name, summary, source_label, source_ref,
    rights_note, expires_at, publication_status,
    COALESCE(reviewed_by::text, ''), reviewed_at, COALESCE(review_note, ''),
    created_at, updated_at`

func scanCommunity(row scanner) (community.Record, error) {
	var record community.Record
	err := row.Scan(&record.ID, &record.CityID, &record.OwnerID,
		&record.PlaceID, &record.Name, &record.Summary, &record.SourceLabel,
		&record.SourceURL, &record.RightsNote, &record.ExpiresAt, &record.Status,
		&record.ReviewedBy, &record.ReviewedAt, &record.ReviewNote,
		&record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func (s *Store) SubmitCommunity(ctx context.Context, ownerID, cityID string, input community.Input) (community.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return community.Record{}, err
	}
	defer tx.Rollback(ctx)
	if input.PlaceID != "" {
		var found bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM places
            WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
              AND (expires_at IS NULL OR expires_at > now()))`, input.PlaceID, cityID).Scan(&found)
		if err != nil {
			return community.Record{}, err
		}
		if !found {
			return community.Record{}, community.ErrConflict
		}
	}
	record, err := scanCommunity(tx.QueryRow(ctx, `INSERT INTO communities (
        city_id, owner_account_id, place_id, name, summary, visibility,
        publication_status, owner_confirmed_at, source_label, source_ref,
        rights_note, maintainer_label, expires_at
    )
    SELECT c.id, a.id, NULLIF($3, '')::uuid, $4, $5, 'public',
           'draft', now(), $6, $7, $8,
           COALESCE(NULLIF(a.handle, ''), 'Birdtie community owner'), $9
    FROM cities c JOIN accounts a ON a.id = $2 AND a.status = 'active'
    WHERE c.id = $1 AND c.publication_status = 'published'
    RETURNING `+communityColumns,
		cityID, ownerID, input.PlaceID, input.Name, input.Summary,
		input.SourceLabel, input.SourceURL, input.RightsNote, input.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return community.Record{}, community.ErrForbidden
	}
	if err != nil {
		return community.Record{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'submit', 'community', $2, 'allowed', 'community_review')`, ownerID, record.ID)
	if err != nil {
		return community.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return community.Record{}, err
	}
	return record, nil
}

func (s *Store) ListOwnCommunities(ctx context.Context, ownerID string) ([]community.Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+communityColumns+` FROM communities
        WHERE owner_account_id = $1 ORDER BY created_at DESC, id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]community.Record, 0)
	for rows.Next() {
		record, err := scanCommunity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (s *Store) ListCommunityQueue(ctx context.Context, reviewerID, cityID string) ([]community.Record, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM city_editor_memberships m
        JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
        WHERE m.account_id = $1 AND m.city_id = $2
          AND m.role = 'reviewer' AND m.state = 'active')`, reviewerID, cityID).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, community.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT `+communityColumns+` FROM communities
        WHERE city_id = $1 AND publication_status = 'draft'
          AND owner_confirmed_at IS NOT NULL
        ORDER BY created_at, id LIMIT 100`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]community.Record, 0)
	for rows.Next() {
		record, err := scanCommunity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (s *Store) ReviewCommunity(ctx context.Context, reviewerID, id string, input community.ReviewInput) (community.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return community.Record{}, err
	}
	defer tx.Rollback(ctx)
	record, err := scanCommunity(tx.QueryRow(ctx, `SELECT `+communityColumns+`
        FROM communities WHERE id = $1 AND city_id IN (
            SELECT m.city_id FROM city_editor_memberships m
            JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
            WHERE m.account_id = $2 AND m.role = 'reviewer' AND m.state = 'active'
        ) FOR UPDATE`, id, reviewerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return community.Record{}, community.ErrForbidden
	}
	if err != nil {
		return community.Record{}, err
	}
	if record.OwnerID == reviewerID || record.Status != "draft" {
		return community.Record{}, community.ErrConflict
	}
	status := "hidden"
	if input.Decision == "publish" {
		if !record.ExpiresAt.After(time.Now()) {
			return community.Record{}, community.ErrConflict
		}
		if record.PlaceID != "" {
			var found bool
			err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM places
                WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
                  AND (expires_at IS NULL OR expires_at > now()))`, record.PlaceID, record.CityID).Scan(&found)
			if err != nil {
				return community.Record{}, err
			}
			if !found {
				return community.Record{}, community.ErrConflict
			}
		}
		status = "published"
	} else if input.Decision != "reject" {
		return community.Record{}, community.ErrConflict
	}
	record, err = scanCommunity(tx.QueryRow(ctx, `UPDATE communities g SET
        publication_status = $2, reviewed_by = $3, reviewed_at = now(),
        review_note = $4, verified_at = CASE WHEN $2 = 'published' THEN now() ELSE NULL END,
        maintainer_label = (SELECT COALESCE(NULLIF(a.handle, ''), 'Birdtie city reviewer')
                            FROM accounts a WHERE a.id = $3 AND a.status = 'active'),
        updated_at = now()
        WHERE g.id = $1 AND EXISTS (SELECT 1 FROM accounts a
                                   WHERE a.id = $3 AND a.status = 'active')
        RETURNING `+communityColumns,
		id, status, reviewerID, input.Note))
	if err != nil {
		return community.Record{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'community', $3, 'allowed', 'community_review')`, reviewerID, status, id)
	if err != nil {
		return community.Record{}, err
	}
	detail := "Your group was not published after review."
	if status == "published" {
		detail = "Your group passed city review and was published."
	}
	if err := insertReviewInboxItem(ctx, tx, record.OwnerID, "community", record.ID,
		"Group reviewed", detail); err != nil {
		return community.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return community.Record{}, err
	}
	return record, nil
}

func (s *Store) WithdrawCommunity(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var withdrawnID string
	err = tx.QueryRow(ctx, `UPDATE communities SET publication_status = 'hidden',
        updated_at = now() WHERE id = $1 AND owner_account_id = $2
          AND publication_status IN ('draft', 'published') RETURNING id`, id, ownerID).Scan(&withdrawnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return community.ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'withdraw', 'community', $2, 'allowed', 'owner_request')`, ownerID, withdrawnID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
