package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/jackc/pgx/v5"
)

const inboxColumns = `id, category, title, detail, resource_type,
    resource_id, created_at, read_at`

func scanInboxItem(row scanner) (inbox.Item, error) {
	var item inbox.Item
	err := row.Scan(&item.ID, &item.Category, &item.Title, &item.Detail,
		&item.ResourceType, &item.ResourceID, &item.CreatedAt, &item.ReadAt)
	return item, err
}

func (s *Store) ListInbox(ctx context.Context, ownerID string) ([]inbox.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+inboxColumns+` FROM inbox_items
        WHERE recipient_account_id = $1 ORDER BY created_at DESC, id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]inbox.Item, 0)
	for rows.Next() {
		item, err := scanInboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReadInboxItem(ctx context.Context, ownerID, id string) (inbox.Item, error) {
	item, err := scanInboxItem(s.pool.QueryRow(ctx, `UPDATE inbox_items
        SET read_at = COALESCE(read_at, now())
        WHERE id = $1 AND recipient_account_id = $2 RETURNING `+inboxColumns, id, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return inbox.Item{}, inbox.ErrNotFound
	}
	return item, err
}

func insertReviewInboxItem(ctx context.Context, tx pgx.Tx, ownerID, resourceType, resourceID, title, detail string) error {
	_, err := tx.Exec(ctx, `INSERT INTO inbox_items
        (recipient_account_id, category, title, detail, resource_type, resource_id)
        VALUES ($1, 'updates', $2, $3, $4, $5)
        ON CONFLICT (recipient_account_id, resource_type, resource_id) DO NOTHING`,
		ownerID, title, detail, resourceType, resourceID)
	return err
}
