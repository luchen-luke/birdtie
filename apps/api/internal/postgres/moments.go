package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5"
)

const momentColumns = `id, author_account_id, city_id,
    COALESCE(place_id::text, ''), title, body, occurred_at,
    time_precision, location_precision, visibility, status, revision,
    created_at, updated_at`

func scanMoment(row scanner) (content.Moment, error) {
	var m content.Moment
	err := row.Scan(&m.ID, &m.AuthorAccountID, &m.CityID, &m.PlaceID,
		&m.Title, &m.Body, &m.OccurredAt, &m.TimePrecision,
		&m.LocationPrecision, &m.Visibility, &m.Status, &m.Revision,
		&m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (s *Store) CreateMomentDraft(ctx context.Context, authorID string, input content.MomentInput) (content.Moment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(ctx)
	m, err := scanMoment(tx.QueryRow(ctx, `INSERT INTO moments (
        author_account_id, city_id, place_id, title, body, occurred_at,
        time_precision, location_precision
    )
    SELECT $1, c.id, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8
    FROM cities c WHERE c.id = $2 AND c.publication_status = 'published'
      AND ($3 = '' OR EXISTS (
        SELECT 1 FROM places p WHERE p.id = NULLIF($3, '')::uuid
          AND p.city_id = c.id AND p.publication_status = 'published'
      ))
    RETURNING `+momentColumns, authorID, input.CityID, input.PlaceID,
		input.Title, input.Body, input.OccurredAt, input.TimePrecision,
		input.LocationPrecision))
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, content.ErrConflict
	}
	if err != nil {
		return content.Moment{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'create_draft', 'moment', $2, 'allowed', 'personal_content')`,
		authorID, m.ID)
	if err != nil {
		return content.Moment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}

func (s *Store) ListOwnMoments(ctx context.Context, authorID string) ([]content.Moment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+momentColumns+`
		FROM moments WHERE author_account_id = $1 AND status <> 'withdrawn'
        ORDER BY updated_at DESC, id DESC LIMIT 100`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	moments := make([]content.Moment, 0)
	for rows.Next() {
		m, err := scanMoment(rows)
		if err != nil {
			return nil, err
		}
		moments = append(moments, m)
	}
	return moments, rows.Err()
}

func (s *Store) GetOwnMoment(ctx context.Context, authorID, momentID string) (content.Moment, error) {
	m, err := scanMoment(s.pool.QueryRow(ctx, `SELECT `+momentColumns+`
        FROM moments WHERE id = $1 AND author_account_id = $2`, momentID, authorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, content.ErrNotFound
	}
	return m, err
}

func (s *Store) UpdateMomentDraft(ctx context.Context, authorID, momentID string, revision int64, input content.MomentInput) (content.Moment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(ctx)
	m, err := scanMoment(tx.QueryRow(ctx, `UPDATE moments m
        SET city_id = $4, place_id = NULLIF($5, '')::uuid,
            title = $6, body = $7, occurred_at = $8,
            time_precision = $9, location_precision = $10,
            revision = m.revision + 1, updated_at = now()
        WHERE m.id = $1 AND m.author_account_id = $2
          AND m.revision = $3 AND m.status = 'draft'
          AND EXISTS (
            SELECT 1 FROM cities c WHERE c.id = $4
              AND c.publication_status = 'published'
          )
          AND ($5 = '' OR EXISTS (
            SELECT 1 FROM places p WHERE p.id = NULLIF($5, '')::uuid
              AND p.city_id = $4 AND p.publication_status = 'published'
          ))
        RETURNING `+momentColumns, momentID, authorID, revision,
		input.CityID, input.PlaceID, input.Title, input.Body, input.OccurredAt,
		input.TimePrecision, input.LocationPrecision))
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, content.ErrConflict
	}
	if err != nil {
		return content.Moment{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'update_draft', 'moment', $2, 'allowed', 'personal_content')`,
		authorID, momentID)
	if err != nil {
		return content.Moment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}

func (s *Store) WithdrawMoment(ctx context.Context, authorID, momentID string, revision int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `UPDATE moments
        SET status = 'withdrawn', visibility = 'private',
            revision = revision + 1, updated_at = now()
        WHERE id = $1 AND author_account_id = $2 AND revision = $3
          AND status <> 'withdrawn'
        RETURNING id`, momentID, authorID, revision).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return content.ErrConflict
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'withdraw', 'moment', $2, 'allowed', 'personal_content')`,
		authorID, momentID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
