package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/jackc/pgx/v5"
)

const intentColumns = `id, city_id, owner_account_id, topic, details,
    available_from, available_until, time_zone, coarse_area_label,
    COALESCE(public_map_zone, ''),
    audience, state, expires_at, COALESCE(reviewed_by::text, ''),
    reviewed_at, COALESCE(review_note, ''), created_at, updated_at`

func scanIntent(row scanner) (intent.Record, error) {
	var record intent.Record
	err := row.Scan(&record.ID, &record.CityID, &record.OwnerID,
		&record.Topic, &record.Details, &record.AvailableFrom,
		&record.AvailableUntil, &record.TimeZone, &record.CoarseAreaLabel,
		&record.PublicMapZone,
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
	var mapReady bool
	err = tx.QueryRow(ctx, `SELECT p.visibility = 'public',
        c.map_center_latitude IS NOT NULL AND c.map_center_longitude IS NOT NULL
        FROM user_profiles p
        JOIN accounts a ON a.id = p.account_id AND a.status = 'active'
        JOIN cities c ON c.id = $2 AND c.publication_status = 'published'
        WHERE p.account_id = $1 FOR UPDATE OF a`, ownerID, cityID).Scan(&public, &mapReady)
	if errors.Is(err, pgx.ErrNoRows) {
		return intent.Record{}, intent.ErrForbidden
	}
	if err != nil {
		return intent.Record{}, err
	}
	if !public {
		return intent.Record{}, intent.ErrConflict
	}
	if input.PublicMapZone != "" && !mapReady {
		return intent.Record{}, intent.ErrConflict
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM intents WHERE owner_account_id = $1
        AND state = 'active' AND expires_at > now()`, ownerID).Scan(&count)
	if err != nil {
		return intent.Record{}, err
	}
	if count >= 3 {
		return intent.Record{}, intent.ErrConflict
	}
	record, err := scanIntent(tx.QueryRow(ctx, `INSERT INTO intents
        (owner_account_id, city_id, topic, details, available_from, available_until,
         time_zone, coarse_area_label, public_map_zone, audience, state,
         expires_at, owner_confirmed_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''),
                'public', 'active', $10, now())
        RETURNING `+intentColumns, ownerID, cityID, input.Topic, input.Details,
		input.AvailableFrom, input.AvailableUntil, input.TimeZone,
		input.CoarseAreaLabel, input.PublicMapZone, input.ExpiresAt))
	if err != nil {
		return intent.Record{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
		VALUES ($1, 'publish', 'intent', $2, 'allowed', 'owner_confirmed')`, ownerID, record.ID)
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
