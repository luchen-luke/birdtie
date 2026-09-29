package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const candidateColumns = `id, city_id, submitted_by, name, category_code,
    summary, latitude, longitude, location_precision, source_label, source_url,
    rights_note, provider_code, provider_place_id, attribution, expires_at,
    status, reviewed_by, reviewed_at, review_note, resolved_place_id, created_at`

func scanCandidate(row scanner) (cityseed.Candidate, error) {
	var c cityseed.Candidate
	err := row.Scan(&c.ID, &c.CityID, &c.SubmittedBy, &c.Name,
		&c.CategoryCode, &c.Summary, &c.Latitude, &c.Longitude,
		&c.LocationPrecision, &c.SourceLabel, &c.SourceURL, &c.RightsNote,
		&c.ProviderCode, &c.ProviderPlaceID, &c.Attribution,
		&c.ExpiresAt, &c.Status, &c.ReviewedBy, &c.ReviewedAt,
		&c.ReviewNote, &c.ResolvedPlaceID, &c.CreatedAt)
	return c, err
}

func (s *Store) Submit(ctx context.Context, actorID, cityID string, input cityseed.SubmitInput) (cityseed.Candidate, error) {
	var providerCode, providerPlaceID, attribution any
	if input.ProviderCode != "" {
		providerCode, providerPlaceID, attribution =
			input.ProviderCode, input.ProviderPlaceID, input.Attribution
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `INSERT INTO place_candidates (
        city_id, submitted_by, name, category_code, summary, latitude, longitude,
        location_precision, source_label, source_url, rights_note,
        provider_code, provider_place_id, attribution, expires_at
    )
    SELECT c.id, a.id, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        $12, $13, $14, $15
    FROM city_editor_memberships m
    JOIN cities c ON c.id = m.city_id AND c.publication_status = 'published'
    JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
    WHERE m.city_id = $1 AND m.account_id = $2 AND m.state = 'active'
    RETURNING `+candidateColumns,
		cityID, actorID, input.Name, input.CategoryCode, input.Summary,
		input.Latitude, input.Longitude, input.LocationPrecision,
		input.SourceLabel, input.SourceURL, input.RightsNote,
		providerCode, providerPlaceID, attribution, input.ExpiresAt)
	c, err := scanCandidate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.Candidate{}, seedWriteError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'submit', 'place_candidate', $2, 'allowed', 'city_seed')`,
		actorID, c.ID)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.Candidate{}, err
	}
	return c, nil
}

func (s *Store) List(ctx context.Context, actorID, cityID string) ([]cityseed.Candidate, error) {
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
	rows, err := s.pool.Query(ctx, `SELECT `+candidateColumns+`
        FROM place_candidates WHERE city_id = $1 AND status = 'pending'
        ORDER BY created_at, id LIMIT 100`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]cityseed.Candidate, 0)
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (s *Store) Review(ctx context.Context, actorID, candidateID string, input cityseed.ReviewInput) (cityseed.Candidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanCandidate(tx.QueryRow(ctx, `SELECT `+candidateColumns+`
        FROM place_candidates
        WHERE id = $1 AND city_id IN (
            SELECT m.city_id FROM city_editor_memberships m
            JOIN accounts a ON a.id = m.account_id
            WHERE m.account_id = $2 AND m.role = 'reviewer'
              AND m.state = 'active' AND a.status = 'active'
        ) FOR UPDATE`, candidateID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.Candidate{}, err
	}
	if c.SubmittedBy == actorID || c.Status != "pending" {
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	if input.Decision != "reject" && !c.ExpiresAt.After(time.Now()) {
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	var resolvedPlaceID any
	switch input.Decision {
	case "publish":
		if input.TargetPlaceID != "" {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		var placeID string
		if err := tx.QueryRow(ctx, `INSERT INTO places (
            id, city_id, name, category_code, summary, latitude, longitude,
            location_precision, publication_status, source_label, source_ref,
            maintainer_label, maintainer_account_id, verified_at, expires_at
        )
        SELECT gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, 'published',
            $8, $9, COALESCE(NULLIF(a.handle, ''), 'Birdtie city editor'),
            a.id, now(), $10
        FROM accounts a WHERE a.id = $11 AND a.status = 'active'
        RETURNING id`, c.CityID, c.Name, c.CategoryCode, c.Summary,
			c.Latitude, c.Longitude, c.LocationPrecision, c.SourceLabel,
			c.SourceURL, c.ExpiresAt, actorID).Scan(&placeID); err != nil {
			return cityseed.Candidate{}, seedWriteError(err)
		}
		resolvedPlaceID = placeID
	case "link_existing":
		if input.TargetPlaceID == "" {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		var placeID string
		err := tx.QueryRow(ctx, `SELECT id FROM places
            WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
            FOR SHARE`, input.TargetPlaceID, c.CityID).Scan(&placeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		if err != nil {
			return cityseed.Candidate{}, err
		}
		resolvedPlaceID = placeID
		_, err = tx.Exec(ctx, `INSERT INTO place_aliases (place_id, locale, alias, source_ref)
            SELECT p.id, 'und', $2, $3 FROM places p
            WHERE p.id = $1 AND p.name <> $2
            ON CONFLICT DO NOTHING`, placeID, c.Name, c.SourceURL)
		if err != nil {
			return cityseed.Candidate{}, err
		}
	case "reject":
		if input.TargetPlaceID != "" {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
	default:
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	if resolvedPlaceID != nil {
		if err := s.linkCandidateSource(ctx, tx, c, actorID, resolvedPlaceID); err != nil {
			return cityseed.Candidate{}, err
		}
		if input.Decision == "link_existing" {
			_, err = tx.Exec(ctx, `UPDATE places p
                SET source_label = $2, source_ref = $3,
                    maintainer_label = COALESCE(NULLIF(a.handle, ''), 'Birdtie city editor'),
                    maintainer_account_id = a.id, updated_at = now(),
                    verified_at = now(), expires_at = $4
                FROM accounts a
                WHERE p.id = $1 AND a.id = $5
                  AND (p.expires_at IS NULL OR p.expires_at < $4)`,
				resolvedPlaceID, c.SourceLabel, c.SourceURL, c.ExpiresAt, actorID)
			if err != nil {
				return cityseed.Candidate{}, err
			}
		}
	}
	status := map[string]string{
		"publish":       "published",
		"link_existing": "linked_duplicate",
		"reject":        "rejected",
	}[input.Decision]
	c, err = scanCandidate(tx.QueryRow(ctx, `UPDATE place_candidates
        SET status = $2, reviewed_by = $3, reviewed_at = now(),
            review_note = $4, resolved_place_id = $5
        WHERE id = $1 AND status = 'pending'
        RETURNING `+candidateColumns,
		candidateID, status, actorID, input.Note, resolvedPlaceID))
	if err != nil {
		return cityseed.Candidate{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'place_candidate', $3, 'allowed', 'city_seed')`,
		actorID, status, candidateID)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.Candidate{}, err
	}
	return c, nil
}

func (s *Store) linkCandidateSource(ctx context.Context, tx pgx.Tx, c cityseed.Candidate, reviewerID string, placeID any) error {
	_, err := tx.Exec(ctx, `INSERT INTO place_sources (
        place_id, candidate_id, source_label, source_url, rights_note,
        reviewer_account_id, verified_at, expires_at
    ) VALUES ($1, $2, $3, $4, $5, $6, now(), $7)`,
		placeID, c.ID, c.SourceLabel, c.SourceURL, c.RightsNote,
		reviewerID, c.ExpiresAt)
	if err != nil {
		return seedWriteError(err)
	}
	if c.ProviderCode != nil {
		var linkedPlaceID string
		err = tx.QueryRow(ctx, `INSERT INTO place_external_refs (
            place_id, provider_code, provider_place_id, source_url,
            attribution, rights_basis, retrieved_at
        ) VALUES ($1, $2, $3, $4, $5, $6, now())
        ON CONFLICT (provider_code, provider_place_id)
        DO UPDATE SET place_id = EXCLUDED.place_id
        WHERE place_external_refs.place_id = EXCLUDED.place_id
        RETURNING place_id`, placeID, c.ProviderCode, c.ProviderPlaceID,
			c.SourceURL, c.Attribution, c.RightsNote).Scan(&linkedPlaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cityseed.ErrDuplicate
		}
		if err != nil {
			return seedWriteError(err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO city_seed_items (
        city_id, place_id, maintainer_account_id, valid_until
    ) VALUES ($1, $2, $3, $4)
    ON CONFLICT (city_id, place_id) DO UPDATE
    SET valid_until = GREATEST(city_seed_items.valid_until, EXCLUDED.valid_until),
        state = 'active', updated_at = now()`,
		c.CityID, placeID, reviewerID, c.ExpiresAt)
	return err
}

func seedWriteError(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return cityseed.ErrDuplicate
	}
	return err
}
