package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5"
)

const momentColumns = `m.id, m.author_account_id, m.city_id,
    COALESCE(m.place_id::text, ''), m.title, m.body, m.occurred_at,
    m.time_precision, m.location_precision,
    ARRAY(SELECT l.activity_id::text FROM moment_activity_links l
          WHERE l.moment_id=m.id ORDER BY l.activity_id),
    COALESCE((SELECT l.community_id::text FROM moment_community_links l
              WHERE l.moment_id=m.id), ''),
    COALESCE((SELECT l.organization_id::text FROM moment_organization_links l
              WHERE l.moment_id=m.id), ''),
    m.visibility, m.status, m.revision, m.created_at, m.updated_at`

func scanMoment(row scanner) (content.Moment, error) {
	var m content.Moment
	err := row.Scan(&m.ID, &m.AuthorAccountID, &m.CityID, &m.PlaceID,
		&m.Title, &m.Body, &m.OccurredAt, &m.TimePrecision,
		&m.LocationPrecision, &m.ActivityIDs, &m.CommunityID,
		&m.OrganizationID, &m.Visibility, &m.Status, &m.Revision,
		&m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (s *Store) CreateMomentDraft(ctx context.Context, authorID string, input content.MomentInput) (content.Moment, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(ctx)
	m, err := createMomentDraftInTx(ctx, tx, authorID, input)
	if err != nil {
		return content.Moment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}

func createMomentDraftInTx(ctx context.Context, tx pgx.Tx, authorID string, input content.MomentInput) (content.Moment, error) {
	m, err := scanMoment(tx.QueryRow(ctx, `INSERT INTO moments AS m (
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
	if err = setMomentContexts(ctx, tx, authorID, m, input); err != nil {
		return content.Moment{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'create_draft', 'moment', $2, 'allowed', 'personal_content')`,
		authorID, m.ID)
	if err != nil {
		return content.Moment{}, err
	}
	if err = appendMomentOutboxTx(ctx, tx, m.ID, agentoutbox.MomentCreated); err != nil {
		return content.Moment{}, err
	}
	return getOwnMomentInTx(ctx, tx, authorID, m.ID)
}

func (s *Store) ListOwnMoments(ctx context.Context, authorID string) ([]content.Moment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+momentColumns+`
		FROM moments m WHERE m.author_account_id = $1 AND m.status <> 'withdrawn'
        ORDER BY m.updated_at DESC, m.id DESC LIMIT 100`, authorID)
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
		FROM moments m WHERE m.id = $1 AND m.author_account_id = $2`, momentID, authorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, content.ErrNotFound
	}
	return m, err
}

func (s *Store) UpdateMomentDraft(ctx context.Context, authorID, momentID string, revision int64, input content.MomentInput) (content.Moment, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(ctx)
	m, err := updateMomentDraftInTx(ctx, tx, authorID, momentID, revision, input)
	if err != nil {
		return content.Moment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}

func updateMomentDraftInTx(ctx context.Context, tx pgx.Tx, authorID, momentID string, revision int64, input content.MomentInput) (content.Moment, error) {
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
          AND ($5 = '' OR m.place_id = NULLIF($5, '')::uuid OR EXISTS (
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
	if err = setMomentContexts(ctx, tx, authorID, m, input); err != nil {
		return content.Moment{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'update_draft', 'moment', $2, 'allowed', 'personal_content')`,
		authorID, momentID)
	if err != nil {
		return content.Moment{}, err
	}
	if err = appendMomentOutboxTx(ctx, tx, m.ID, agentoutbox.MomentUpdated); err != nil {
		return content.Moment{}, err
	}
	return getOwnMomentInTx(ctx, tx, authorID, m.ID)
}

// Context changes are explicit. Omitted fields preserve old-client links;
// an empty string removes that one relation. Everything shares the draft
// update transaction, so an invalid reference rolls the draft revision back.
func setMomentContexts(ctx context.Context, tx pgx.Tx, authorID string, m content.Moment, in content.MomentInput) error {
	if in.ActivityID != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM moment_activity_links WHERE moment_id=$1`, m.ID); err != nil {
			return err
		}
		if *in.ActivityID != "" {
			tag, err := tx.Exec(ctx, `INSERT INTO moment_activity_links
                (moment_id,activity_id,city_id,author_confirmed_at)
                SELECT $1,a.id,a.city_id,now() FROM activities a
                WHERE a.id=$2 AND a.city_id=$3 AND a.publication_status='published'
                  AND birdtie_activity_visible_to(a.id,$4)`, m.ID, *in.ActivityID, m.CityID, authorID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return content.ErrConflict
			}
		}
	}
	if in.CommunityID != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM moment_community_links WHERE moment_id=$1`, m.ID); err != nil {
			return err
		}
		if *in.CommunityID != "" {
			tag, err := tx.Exec(ctx, `INSERT INTO moment_community_links
                (moment_id,community_id,author_confirmed_at)
                SELECT $1,c.id,now() FROM communities c
                JOIN community_memberships cm ON cm.community_id=c.id
                WHERE c.id=$2 AND (c.city_id IS NULL OR c.city_id=$3)
                  AND c.lifecycle_status='active' AND c.publication_status='published'
                  AND cm.user_account_id=$4 AND cm.status='active'`, m.ID, *in.CommunityID, m.CityID, authorID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return content.ErrConflict
			}
		}
	}
	if in.OrganizationID != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM moment_organization_links WHERE moment_id=$1`, m.ID); err != nil {
			return err
		}
		if *in.OrganizationID != "" {
			tag, err := tx.Exec(ctx, `INSERT INTO moment_organization_links
                (moment_id,organization_id,author_confirmed_at)
                SELECT $1,o.id,now() FROM organizations o
                JOIN organization_memberships om ON om.organization_id=o.id
                WHERE o.id=$2 AND o.status='active' AND om.user_account_id=$3
                  AND om.status='active'`, m.ID, *in.OrganizationID, authorID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return content.ErrConflict
			}
		}
	}
	return nil
}

func (s *Store) WithdrawMoment(ctx context.Context, authorID, momentID string, revision int64) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = withdrawMomentInTx(ctx, tx, authorID, momentID, revision); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func withdrawMomentInTx(ctx context.Context, tx pgx.Tx, authorID, momentID string, revision int64) error {
	var id string
	err := tx.QueryRow(ctx, `UPDATE moments
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
	if err = appendMomentOutboxTx(ctx, tx, momentID, agentoutbox.MomentWithdrawn); err != nil {
		return err
	}
	return nil
}

func getOwnMomentInTx(ctx context.Context, tx pgx.Tx, authorID, momentID string) (content.Moment, error) {
	m, err := scanMoment(tx.QueryRow(ctx, `SELECT `+momentColumns+` FROM moments m WHERE m.id=$1 AND m.author_account_id=$2`, momentID, authorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, content.ErrNotFound
	}
	return m, err
}
