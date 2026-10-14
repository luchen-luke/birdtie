package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListBlocks(ctx context.Context, ownerID string) ([]identity.Block, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.blocked_account_id,b.created_at,COALESCE(e.version,0)
        FROM account_blocks b LEFT JOIN person_block_control_epochs e ON e.owner_account_id=b.blocker_account_id AND e.target_account_id=b.blocked_account_id WHERE b.blocker_account_id = $1
        ORDER BY b.created_at DESC,b.blocked_account_id LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blocks := make([]identity.Block, 0)
	for rows.Next() {
		var block identity.Block
		if err := rows.Scan(&block.AccountID, &block.CreatedAt, &block.Version); err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

func blockAccountEffectTx(ctx context.Context, tx pgx.Tx, actorID, targetID string) error {
	if actorID == targetID {
		return identity.ErrInvalidGrant
	}
	var target string
	err := tx.QueryRow(ctx, `SELECT id FROM accounts
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
        SET revoked_at = now(), revision = LEAST(revision + 1,8999999999999999)
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
	return nil
}
func unblockAccountEffectTx(ctx context.Context, tx pgx.Tx, actorID, targetID string) error {
	var deleted string
	err := tx.QueryRow(ctx, `DELETE FROM account_blocks
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
	return nil
}
func (s *Store) BlockAccount(ctx context.Context, actorID, targetID string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if e = blockAccountEffectTx(ctx, tx, actorID, targetID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) UnblockAccount(ctx context.Context, actorID, targetID string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if e = unblockAccountEffectTx(ctx, tx, actorID, targetID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

var _ identity.BlockControlStore = (*Store)(nil)

func blockControlLock(ctx context.Context, f humanWriteFrame, target string) error {
	_, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,142))`, f.a.ID+":BLOCKPAIR:"+target)
	if e != nil {
		return identity.ErrControlUnavailable
	}
	return nil
}
func blockControlSnapshot(ctx context.Context, f humanWriteFrame, target string, lock bool) (int64, bool, bool, string, error) {
	if !identity.HumanControlUUID(target) || target == f.a.ID {
		return 0, false, false, "", identity.ErrControlInvalid
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var active bool
	var targetEpoch string
	e := f.tx.QueryRow(ctx, `SELECT status='active',xmin::text FROM accounts WHERE id=$1`+suffix, target).Scan(&active, &targetEpoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, false, false, "", identity.ErrNotFound
	}
	if e != nil {
		return 0, false, false, "", identity.ErrControlUnavailable
	}
	var version int64
	var blocked bool
	var pairEpoch, blockEpoch string
	e = f.tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM person_block_control_epochs WHERE owner_account_id=$1 AND target_account_id=$2),0),EXISTS(SELECT 1 FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2),COALESCE((SELECT xmin::text FROM person_block_control_epochs WHERE owner_account_id=$1 AND target_account_id=$2),''),COALESCE((SELECT xmin::text FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2),'')`, f.a.ID, target).Scan(&version, &blocked, &pairEpoch, &blockEpoch)
	if e != nil {
		return 0, false, false, "", identity.ErrControlUnavailable
	}
	return version, blocked, active, humanWriteHashJSON([]any{target, targetEpoch, version, blocked, pairEpoch, blockEpoch}), nil
}
func blockControlSource(ctx context.Context, f humanWriteFrame, in identity.BlockControlInput, lock bool) (string, error) {
	version, blocked, active, source, e := blockControlSnapshot(ctx, f, in.AccountID, lock)
	if e != nil {
		return "", e
	}
	if version != in.ExpectedVersion || (in.Action == "BLOCK" && (blocked || !active)) || (in.Action == "UNBLOCK" && !blocked) {
		return "", identity.ErrControlChanged
	}
	return source, nil
}
func (s *Store) ReadBlockControl(ctx context.Context, d [32]byte, a identity.Actor, target string) (identity.BlockControlState, error) {
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "BLOCK", "")
	if e != nil {
		return identity.BlockControlState{}, e
	}
	defer f.tx.Rollback(context.Background())
	version, blocked, active, source, e := blockControlSnapshot(ctx, f, target, false)
	if e != nil {
		return identity.BlockControlState{}, e
	}
	now, until, e := s.humanWriteNow(ctx, f, nil)
	if e != nil {
		return identity.BlockControlState{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		_, _, _, current, e := blockControlSnapshot(ctx, next, target, true)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error { return check(ctx, f) })); e != nil {
		return identity.BlockControlState{}, e
	}
	return identity.BlockControlState{AccountID: target, Version: version, Blocked: blocked, CanBlock: active && !blocked && version < identity.HumanControlMaxVersion, CanUnblock: blocked, ObservedAt: now, ExpiresAt: until, ResponseProof: humanWriteAttachProof(s, f, until, check)}, nil
}
func (s *Store) PreviewBlockControl(ctx context.Context, d [32]byte, a identity.Actor, in identity.BlockControlInput) (identity.BlockControlPreview, error) {
	in, e := identity.NormalizeBlockControl(in, false)
	if e != nil {
		return identity.BlockControlPreview{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "BLOCK", in.OperationID)
	if e != nil {
		return identity.BlockControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = humanWriteReceiptTx(ctx, f); e == nil {
		return identity.BlockControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return identity.BlockControlPreview{}, identity.ErrControlUnavailable
	}
	source, e := blockControlSource(ctx, f, in, false)
	if e != nil {
		return identity.BlockControlPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, "", in.Action, identity.BlockControlDigest(in), source, nil)
	if e != nil {
		return identity.BlockControlPreview{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		current, e := blockControlSource(ctx, next, in, false)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error { return check(ctx, f) })); e != nil {
		return identity.BlockControlPreview{}, e
	}
	base.ResponseProof = humanWriteAttachProof(s, f, base.ExpiresAt, check)
	return identity.BlockControlPreview{HumanControlPreview: base, AccountID: in.AccountID, ExpectedVersion: in.ExpectedVersion}, nil
}
func (s *Store) CommitBlockControl(ctx context.Context, d [32]byte, a identity.Actor, in identity.BlockControlInput) (identity.HumanControlReceipt, error) {
	in, e := identity.NormalizeBlockControl(in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "BLOCK", in.OperationID)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := identity.BlockControlDigest(in)
	if r, done, e := s.humanWriteReplay(ctx, f, "", digest); done {
		return r, e
	}
	if e = blockControlLock(ctx, f, in.AccountID); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	source, e := blockControlSource(ctx, f, in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	deadline, e := s.humanWriteApprove(ctx, f, "", in.Action, digest, source, in.Approval, nil)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	if in.Action == "BLOCK" {
		e = blockAccountEffectTx(ctx, f.tx, a.ID, in.AccountID)
	} else {
		e = unblockAccountEffectTx(ctx, f.tx, a.ID, in.AccountID)
	}
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	version, blocked, _, post, e := blockControlSnapshot(ctx, f, in.AccountID, true)
	if e != nil || version != identity.HumanControlNextVersion(in.ExpectedVersion) || blocked != (in.Action == "BLOCK") {
		return identity.HumanControlReceipt{}, identity.ErrControlChanged
	}
	r, e := humanWriteSaveReceipt(ctx, f, "", in.Action, in.AccountID, "", digest, version)
	if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, deadline, func() error {
		_, _, _, current, e := blockControlSnapshot(ctx, f, in.AccountID, true)
		if e != nil {
			return e
		}
		if current != post {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	return r, nil
}
func (s *Store) ReadBlockOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "BLOCK", op, "", false)
}
func (s *Store) StopBlockOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "BLOCK", op, "", true)
}
