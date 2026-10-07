package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListBlocks(ctx context.Context, ownerID string) ([]identity.Block, error) {
	rows, err := s.pool.Query(ctx, `SELECT blocked_account_id, created_at
        FROM account_blocks WHERE blocker_account_id = $1
        ORDER BY created_at DESC, blocked_account_id LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blocks := make([]identity.Block, 0)
	for rows.Next() {
		var block identity.Block
		if err := rows.Scan(&block.AccountID, &block.CreatedAt); err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

func (s *Store) BlockAccount(ctx context.Context, actorID, targetID string) error {
	if actorID == targetID {
		return identity.ErrInvalidGrant
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var target string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts
        WHERE id = $1 AND status = 'active' FOR SHARE`, targetID).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	if err != nil {
		return err
	}
	inserted, err := tx.Exec(ctx, `INSERT INTO account_blocks (
        blocker_account_id, blocked_account_id
    ) VALUES ($1, $2) ON CONFLICT DO NOTHING`, actorID, targetID)
	if err != nil {
		return err
	}
	_, err = auditExec(ctx, tx, `WITH removed AS (
		UPDATE person_ties SET status='removed',updated_at=now()
		WHERE status='active' AND
		  ((person_a_account_id=$1 AND person_b_account_id=$2) OR
		   (person_a_account_id=$2 AND person_b_account_id=$1))
		RETURNING id)
		INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
		SELECT $1,'remove','person_tie',id::text,'allowed','personal_safety' FROM removed`, actorID, targetID)
	if err != nil {
		return err
	}
	_, err = auditExec(ctx, tx, `WITH ended AS (
		UPDATE connection_requests SET
		  state=CASE WHEN sender_account_id=$1 THEN 'withdrawn' ELSE 'declined' END,
		  decided_at=now()
		WHERE state='pending' AND
		  ((sender_account_id=$1 AND recipient_account_id=$2) OR
		   (sender_account_id=$2 AND recipient_account_id=$1))
		RETURNING id)
		INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
		SELECT $1,'end','connection_request',id::text,'allowed','personal_safety' FROM ended`, actorID, targetID)
	if err != nil {
		return err
	}
	_, err = auditExec(ctx, tx, `WITH revoked AS (UPDATE consent_grants
        SET revoked_at = now(), revision = revision + 1
        WHERE revoked_at IS NULL AND recipient_account_id IS NOT NULL
          AND ((owner_account_id = $1 AND recipient_account_id = $2)
            OR (owner_account_id = $2 AND recipient_account_id = $1))
        RETURNING id,resource_type,resource_id,purpose)
        INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose,target_resource_id)
        SELECT $1,'revoke',resource_type,resource_id,'allowed','personal_safety',id FROM revoked`,
		actorID, targetID)
	if err != nil {
		return err
	}
	if inserted.RowsAffected() == 1 {
		_, err = auditExec(ctx, tx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'block', 'account', $2, 'allowed', 'personal_safety')`,
			actorID, targetID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) UnblockAccount(ctx context.Context, actorID, targetID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var deleted string
	err = tx.QueryRow(ctx, `DELETE FROM account_blocks
        WHERE blocker_account_id = $1 AND blocked_account_id = $2
        RETURNING blocked_account_id`, actorID, targetID).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events (
        actor_account_id, action, resource_type, resource_id, decision, purpose
    ) VALUES ($1, 'unblock', 'account', $2, 'allowed', 'personal_safety')`,
		actorID, targetID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
