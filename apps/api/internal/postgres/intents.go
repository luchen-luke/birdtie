package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/jackc/pgx/v5"
)

const intentColumns = `id, city_id, owner_account_id, topic, details,
    available_from, available_until, time_zone, coarse_area_label,
    audience, state, expires_at, COALESCE(reviewed_by::text, ''),
    reviewed_at, COALESCE(review_note, ''), created_at, updated_at`

func scanIntent(row scanner) (intent.Record, error) {
	var record intent.Record
	err := row.Scan(&record.ID, &record.CityID, &record.OwnerID,
		&record.Topic, &record.Details, &record.AvailableFrom,
		&record.AvailableUntil, &record.TimeZone, &record.CoarseAreaLabel,
		&record.Audience, &record.State, &record.ExpiresAt,
		&record.ReviewedBy, &record.ReviewedAt, &record.ReviewNote,
		&record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func (s *Store) SubmitIntent(ctx context.Context, ownerID, cityID string, input intent.Input) (intent.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return intent.Record{}, err
	}
	defer tx.Rollback(ctx)
	var public bool
	err = tx.QueryRow(ctx, `SELECT p.visibility = 'public' FROM user_profiles p
        JOIN accounts a ON a.id = p.account_id AND a.status = 'active'
        JOIN cities c ON c.id = $2 AND c.publication_status = 'published'
        WHERE p.account_id = $1 FOR UPDATE OF a`, ownerID, cityID).Scan(&public)
	if errors.Is(err, pgx.ErrNoRows) {
		return intent.Record{}, intent.ErrForbidden
	}
	if err != nil {
		return intent.Record{}, err
	}
	if !public {
		return intent.Record{}, intent.ErrConflict
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM intents WHERE owner_account_id = $1
        AND state IN ('draft', 'active') AND expires_at > now()`, ownerID).Scan(&count)
	if err != nil {
		return intent.Record{}, err
	}
	if count >= 3 {
		return intent.Record{}, intent.ErrConflict
	}
	record, err := scanIntent(tx.QueryRow(ctx, `INSERT INTO intents
        (owner_account_id, city_id, topic, details, available_from, available_until,
         time_zone, coarse_area_label, audience, state, expires_at, owner_confirmed_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'public', 'draft', $9, now())
        RETURNING `+intentColumns, ownerID, cityID, input.Topic, input.Details,
		input.AvailableFrom, input.AvailableUntil, input.TimeZone,
		input.CoarseAreaLabel, input.ExpiresAt))
	if err != nil {
		return intent.Record{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'submit', 'intent', $2, 'allowed', 'intent_review')`, ownerID, record.ID)
	if err != nil {
		return intent.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return intent.Record{}, err
	}
	return record, nil
}

func (s *Store) ListOwnIntents(ctx context.Context, ownerID string) ([]intent.Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+intentColumns+` FROM intents
        WHERE owner_account_id = $1 ORDER BY created_at DESC, id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]intent.Record, 0)
	for rows.Next() {
		record, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (s *Store) ListIntentQueue(ctx context.Context, reviewerID, cityID string) ([]intent.Record, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM city_editor_memberships m
        JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
        WHERE m.account_id = $1 AND m.city_id = $2 AND m.role = 'reviewer'
          AND m.state = 'active')`, reviewerID, cityID).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, intent.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT `+intentColumns+` FROM intents
        WHERE city_id = $1 AND state = 'draft' AND audience = 'public'
          AND owner_confirmed_at IS NOT NULL AND expires_at > now()
        ORDER BY created_at, id LIMIT 100`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]intent.Record, 0)
	for rows.Next() {
		record, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (s *Store) ReviewIntent(ctx context.Context, reviewerID, id string, input intent.ReviewInput) (intent.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return intent.Record{}, err
	}
	defer tx.Rollback(ctx)
	record, err := scanIntent(tx.QueryRow(ctx, `SELECT `+intentColumns+` FROM intents
		WHERE id = $1 AND owner_confirmed_at IS NOT NULL
		  AND city_id IN (SELECT m.city_id FROM city_editor_memberships m
            JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
            WHERE m.account_id = $2 AND m.role = 'reviewer' AND m.state = 'active')
        FOR UPDATE`, id, reviewerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return intent.Record{}, intent.ErrForbidden
	}
	if err != nil {
		return intent.Record{}, err
	}
	if record.OwnerID == reviewerID || record.State != "draft" ||
		record.Audience != "public" {
		return intent.Record{}, intent.ErrConflict
	}
	state := "withdrawn"
	if input.Decision != "publish" && input.Decision != "reject" {
		return intent.Record{}, intent.ErrConflict
	}
	if input.Decision == "publish" {
		var eligible bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM accounts a
            JOIN user_profiles p ON p.account_id = a.id AND p.visibility = 'public'
            JOIN cities c ON c.id = $2 AND c.publication_status = 'published'
            WHERE a.id = $1 AND a.status = 'active')`, record.OwnerID, record.CityID).Scan(&eligible)
		if err != nil {
			return intent.Record{}, err
		}
		if !eligible || !record.AvailableUntil.After(time.Now()) ||
			!record.ExpiresAt.After(time.Now()) {
			return intent.Record{}, intent.ErrConflict
		}
		state = "active"
	}
	record, err = scanIntent(tx.QueryRow(ctx, `UPDATE intents SET state = $2,
        reviewed_by = $3, reviewed_at = now(), review_note = $4, updated_at = now()
        WHERE id = $1 RETURNING `+intentColumns, id, state, reviewerID, input.Note))
	if err != nil {
		return intent.Record{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'intent', $3, 'allowed', 'intent_review')`, reviewerID, state, id)
	if err != nil {
		return intent.Record{}, err
	}
	detail := "Your intent was not published after city review."
	if state == "active" {
		detail = "Your intent passed city review and is now visible in Birdtie."
	}
	if err := insertReviewInboxItem(ctx, tx, record.OwnerID, "intent", record.ID,
		"Intent reviewed", detail); err != nil {
		return intent.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return intent.Record{}, err
	}
	return record, nil
}

func (s *Store) WithdrawIntent(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var withdrawnID string
	err = tx.QueryRow(ctx, `UPDATE intents SET state = 'withdrawn', updated_at = now()
        WHERE id = $1 AND owner_account_id = $2 AND state IN ('draft', 'active')
        RETURNING id`, id, ownerID).Scan(&withdrawnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return intent.ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'withdraw', 'intent', $2, 'allowed', 'owner_request')`, ownerID, withdrawnID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
