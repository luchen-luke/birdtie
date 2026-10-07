package postgres

import (
	"context"
	"errors"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5"
)

// Save only accepts a target that is currently visible to this owner. It is
// idempotent; a later visibility change cannot turn a bookmark into access.
func (s *Store) Save(ctx context.Context, ownerID, kind, targetID string) (string, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var created bool
	id, e := saveInQuery(ctx, tx, ownerID, kind, targetID, &created)
	if e != nil {
		return "", e
	}
	if created {
		if e = insertDomainAudit(ctx, tx, ownerID, "create", "saved_item", id, "human_bookmark", &targetID); e != nil {
			return "", e
		}
	}
	return id, tx.Commit(ctx)
}

type savedQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func saveInQuery(ctx context.Context, q savedQuery, ownerID, kind, targetID string, created *bool) (string, error) {
	var insert string
	var existing string
	switch kind {
	case "place":
		insert = `INSERT INTO saved_items (owner_account_id, place_id)
            SELECT $1, p.id FROM places p
            JOIN cities c ON c.id = p.city_id AND c.publication_status = 'published'
            WHERE p.id = $2 AND p.publication_status = 'published'
              AND (p.expires_at IS NULL OR p.expires_at > now())
            ON CONFLICT DO NOTHING RETURNING id`
		existing = `SELECT s.id FROM saved_items s
            JOIN places p ON p.id = s.place_id
            JOIN cities c ON c.id = p.city_id AND c.publication_status = 'published'
            WHERE s.owner_account_id = $1 AND s.place_id = $2
              AND p.publication_status = 'published'
              AND (p.expires_at IS NULL OR p.expires_at > now())`
	case "activity":
		insert = `INSERT INTO saved_items (owner_account_id, activity_id)
            SELECT $1, a.id FROM activities a
            JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
            WHERE a.id = $2 AND a.publication_status = 'published'
              AND a.visibility = 'public'
              AND (a.expires_at IS NULL OR a.expires_at > now())
              AND NOT EXISTS (SELECT 1 FROM account_blocks b
                  WHERE a.host_account_id IS NOT NULL AND
                    ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                     OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))
            ON CONFLICT DO NOTHING RETURNING id`
		existing = `SELECT s.id FROM saved_items s
            JOIN activities a ON a.id = s.activity_id
            JOIN cities c ON c.id = a.city_id AND c.publication_status = 'published'
            WHERE s.owner_account_id = $1 AND s.activity_id = $2
              AND a.publication_status = 'published'
              AND a.visibility = 'public'
              AND (a.expires_at IS NULL OR a.expires_at > now())
              AND NOT EXISTS (SELECT 1 FROM account_blocks b
                  WHERE a.host_account_id IS NOT NULL AND
                    ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                     OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))`
	case "group":
		insert = `INSERT INTO saved_items (owner_account_id, community_id)
            SELECT $1, g.id FROM communities g
            JOIN accounts a ON a.id = g.owner_account_id AND a.status = 'active'
            JOIN cities c ON c.id = g.city_id AND c.publication_status = 'published'
            WHERE g.id = $2 AND g.publication_status = 'published'
              AND g.visibility = 'public' AND g.owner_confirmed_at IS NOT NULL
              AND (g.expires_at IS NULL OR g.expires_at > now())
              AND NOT EXISTS (SELECT 1 FROM account_blocks b
                  WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = g.owner_account_id)
                     OR (b.blocker_account_id = g.owner_account_id AND b.blocked_account_id = $1))
            ON CONFLICT DO NOTHING RETURNING id`
		existing = `SELECT s.id FROM saved_items s
            JOIN communities g ON g.id = s.community_id
            JOIN accounts a ON a.id = g.owner_account_id AND a.status = 'active'
            JOIN cities c ON c.id = g.city_id AND c.publication_status = 'published'
            WHERE s.owner_account_id = $1 AND s.community_id = $2
              AND g.publication_status = 'published' AND g.visibility = 'public'
              AND g.owner_confirmed_at IS NOT NULL
              AND (g.expires_at IS NULL OR g.expires_at > now())
              AND NOT EXISTS (SELECT 1 FROM account_blocks b
                  WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = g.owner_account_id)
                     OR (b.blocker_account_id = g.owner_account_id AND b.blocked_account_id = $1))`
	default:
		return "", saved.ErrNotFound
	}
	var id string
	err := q.QueryRow(ctx, insert, ownerID, targetID).Scan(&id)
	if err == nil {
		if created != nil {
			*created = true
		}
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	err = q.QueryRow(ctx, existing, ownerID, targetID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", saved.ErrNotFound
	}
	return id, err
}

func lockSavedTarget(ctx context.Context, tx pgx.Tx, ref ea.Ref) error {
	var table string
	switch ref.Type {
	case "place":
		table = "places"
	case "activity":
		table = "activities"
	case "community":
		table = "communities"
	default:
		return ea.ErrInvalid
	}
	var id string
	if e := tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 FOR UPDATE`, ref.ID).Scan(&id); e != nil {
		return saved.ErrNotFound
	}
	return nil
}
func (s *Store) SaveBound(ctx context.Context, a ea.Access, kind, id string, b ea.BoundCondition) (string, error) {
	ref := ea.Ref{Type: kind, ID: id}
	if kind == "group" {
		ref.Type = "community"
	}
	if b.Kind != ea.Save || b.Operation != "SAVE" {
		return "", ea.ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", ea.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if e = s.lockEntityActionWriter(ctx, tx, a); e != nil {
		return "", e
	}
	if e = lockSavedTarget(ctx, tx, ref); e != nil {
		return "", e
	}
	fence, e := s.checkEntityActionWrite(ctx, tx, a, ref, b)
	if e != nil {
		return "", e
	}
	var created bool
	result, e := saveInQuery(ctx, tx, a.Actor.ID, kind, id, &created)
	if e != nil {
		return "", e
	}
	if created {
		if e = insertDomainAudit(ctx, tx, a.Actor.ID, "create", "saved_item", result, "human_bookmark", &id); e != nil {
			return "", e
		}
	}
	var exact bool
	if e = tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(id=$3::uuid) FROM saved_items WHERE owner_account_id=$1 AND coalesce(place_id,activity_id,community_id)=$2`, a.Actor.ID, id, result).Scan(&exact); e != nil || !exact {
		return "", ea.ErrChanged
	}
	if e = s.finishEntityActionWrite(ctx, tx, fence); e != nil {
		return "", e
	}
	return result, tx.Commit(ctx)
}
func (s *Store) RemoveSavedBound(ctx context.Context, a ea.Access, id string, b ea.BoundCondition) error {
	if b.Kind != ea.Save || b.Operation != "UNSAVE" {
		return ea.ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ea.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if e = s.lockEntityActionWriter(ctx, tx, a); e != nil {
		return e
	}
	var ref ea.Ref
	if e = tx.QueryRow(ctx, `SELECT CASE WHEN place_id IS NOT NULL THEN 'place' WHEN activity_id IS NOT NULL THEN 'activity' ELSE 'community' END,coalesce(place_id,activity_id,community_id)::text FROM saved_items WHERE id=$1 AND owner_account_id=$2`, id, a.Actor.ID).Scan(&ref.Type, &ref.ID); e != nil {
		return saved.ErrNotFound
	}
	if e = lockSavedTarget(ctx, tx, ref); e != nil {
		return e
	}
	var actual string
	if e = tx.QueryRow(ctx, `SELECT id FROM saved_items WHERE id=$1 AND owner_account_id=$2 AND coalesce(place_id,activity_id,community_id)=$3 FOR UPDATE`, id, a.Actor.ID, ref.ID).Scan(&actual); e != nil {
		return saved.ErrNotFound
	}
	fence, e := s.checkEntityActionWrite(ctx, tx, a, ref, b)
	if e != nil {
		return e
	}
	removed, e := tx.Exec(ctx, `DELETE FROM saved_items WHERE id=$1 AND owner_account_id=$2`, actual, a.Actor.ID)
	if e != nil {
		return e
	}
	if removed.RowsAffected() != 1 {
		return ea.ErrChanged
	}
	if e = insertDomainAudit(ctx, tx, a.Actor.ID, "delete", "saved_item", actual, "human_bookmark", &ref.ID); e != nil {
		return e
	}
	var empty bool
	if e = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM saved_items WHERE owner_account_id=$1 AND coalesce(place_id,activity_id,community_id)=$2)`, a.Actor.ID, ref.ID).Scan(&empty); e != nil || !empty {
		return ea.ErrChanged
	}
	if e = s.finishEntityActionWrite(ctx, tx, fence); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *Store) ListSaved(ctx context.Context, ownerID string) ([]saved.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,
            CASE WHEN s.place_id IS NOT NULL THEN 'place'
                 WHEN s.activity_id IS NOT NULL THEN 'activity' ELSE 'group' END,
            COALESCE(s.place_id, s.activity_id, s.community_id)::text,
            COALESCE(p.name, a.title, g.name, ''),
            COALESCE(p.summary, a.summary, g.summary, ''),
            COALESCE(p.city_id, a.city_id, g.city_id, ''),
            (p.id IS NOT NULL OR a.id IS NOT NULL OR g.id IS NOT NULL),
            s.created_at
        FROM saved_items s
        LEFT JOIN places p ON p.id = s.place_id
          AND p.publication_status = 'published'
          AND (p.expires_at IS NULL OR p.expires_at > now())
          AND EXISTS (SELECT 1 FROM cities c WHERE c.id = p.city_id
                      AND c.publication_status = 'published')
        LEFT JOIN activities a ON a.id = s.activity_id
          AND a.publication_status = 'published'
          AND a.visibility = 'public'
          AND (a.expires_at IS NULL OR a.expires_at > now())
          AND EXISTS (SELECT 1 FROM cities c WHERE c.id = a.city_id
                      AND c.publication_status = 'published')
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE a.host_account_id IS NOT NULL AND
                ((b.blocker_account_id = $1 AND b.blocked_account_id = a.host_account_id)
                 OR (b.blocker_account_id = a.host_account_id AND b.blocked_account_id = $1)))
        LEFT JOIN communities g ON g.id = s.community_id
          AND g.publication_status = 'published' AND g.visibility = 'public'
          AND g.owner_confirmed_at IS NOT NULL
          AND (g.expires_at IS NULL OR g.expires_at > now())
          AND EXISTS (SELECT 1 FROM accounts owner WHERE owner.id = g.owner_account_id
                      AND owner.status = 'active')
          AND EXISTS (SELECT 1 FROM cities c WHERE c.id = g.city_id
                      AND c.publication_status = 'published')
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = g.owner_account_id)
                 OR (b.blocker_account_id = g.owner_account_id AND b.blocked_account_id = $1))
        WHERE s.owner_account_id = $1
        ORDER BY s.created_at DESC, s.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]saved.Item, 0)
	for rows.Next() {
		var item saved.Item
		if err := rows.Scan(&item.ID, &item.Kind, &item.TargetID, &item.Title,
			&item.Summary, &item.CityID, &item.Available, &item.SavedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RemoveSaved(ctx context.Context, ownerID, id string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var target string
	e = tx.QueryRow(ctx, `DELETE FROM saved_items WHERE id=$1 AND owner_account_id=$2 RETURNING coalesce(place_id,activity_id,community_id)::text`, id, ownerID).Scan(&target)
	if errors.Is(e, pgx.ErrNoRows) {
		return saved.ErrNotFound
	}
	if e != nil {
		return e
	}
	if e = insertDomainAudit(ctx, tx, ownerID, "delete", "saved_item", id, "human_bookmark", &target); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
