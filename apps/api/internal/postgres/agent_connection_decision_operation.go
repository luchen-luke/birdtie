package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ connection.DecisionOperationStore = (*Store)(nil)
var _ connection.DecisionOperationResponseStore = (*Store)(nil)

type connectionDecisionOperation struct {
	id, digest string
	receipt    connection.DecisionOperationReceipt
}

// The original Request writer owns the effects; this is only its causal record.
func (s *Store) DecideRequestOperation(ctx context.Context, a ea.Access, id, action, operation string) (connection.DecisionOperationReceipt, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	digest, e := connection.DecisionDigest(a.Actor.ID, id, action)
	if e != nil || !connection.ValidDecisionID(operation) {
		return connection.DecisionOperationReceipt{}, connection.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.DecisionOperationReceipt{}, connection.ErrOperationUnavailable
	}
	defer tx.Rollback(context.Background())
	op := &connectionDecisionOperation{id: operation, digest: digest}
	if _, e = decideRequestInTx(ctx, tx, a.Actor.ID, id, action, op); e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return connection.DecisionOperationReceipt{}, connection.ErrOperationUnavailable
	}
	return op.receipt, nil
}

func readConnectionDecisionReceipt(ctx context.Context, tx pgx.Tx, owner, operation string) (connection.DecisionOperationReceipt, error) {
	var r connection.DecisionOperationReceipt
	e := tx.QueryRow(ctx, `SELECT owner_account_id,request_id,operation_id,request_digest,action,scope,status,state,COALESCE(reason,''),recorded_at FROM connection_request_decision_receipts WHERE owner_account_id=$1 AND operation_id=$2`, owner, operation).Scan(&r.OwnerID, &r.RequestID, &r.OperationID, &r.RequestDigest, &r.Action, &r.Scope, &r.Status, &r.State, &r.Reason, &r.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, connection.ErrNotFound
	}
	if e != nil {
		return r, connection.ErrOperationUnavailable
	}
	r.SchemaVersion = connection.DecisionOperationSchema
	r.RecordedAt = r.RecordedAt.UTC()
	return r, nil
}

func connectionDecisionPrior(ctx context.Context, tx pgx.Tx, owner, id, action string, op *connectionDecisionOperation) (bool, error) {
	r, e := readConnectionDecisionReceipt(ctx, tx, owner, op.id)
	if errors.Is(e, connection.ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	// Caller has already checked the actual current Request participant/action.
	if r.RequestID != id || r.Action != action || r.RequestDigest != op.digest {
		return false, connection.ErrOperationChanged
	}
	if e = connection.ValidateDecisionReceipt(r, owner, id, op.id, action); e != nil {
		return false, e
	}
	op.receipt = r
	return true, nil
}

func recordConnectionDecision(ctx context.Context, tx pgx.Tx, owner, id, action, scope, status, reason string, at time.Time, op *connectionDecisionOperation) error {
	r := connection.DecisionOperationReceipt{SchemaVersion: connection.DecisionOperationSchema, OwnerID: owner, RequestID: id, OperationID: op.id, RequestDigest: op.digest, Action: action, Scope: scope, Status: status, Reason: reason, RecordedAt: at.UTC()}
	if status == "COMMITTED" {
		r.State = connection.DecisionState(action)
	}
	if e := connection.ValidateDecisionReceipt(r, owner, id, op.id, action); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `INSERT INTO connection_request_decision_receipts(owner_account_id,request_id,operation_id,request_digest,action,scope,status,state,reason,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10)`, owner, id, op.id, op.digest, action, scope, status, r.State, reason, r.RecordedAt)
	if e != nil || tag.RowsAffected() != 1 {
		return connection.ErrOperationUnavailable
	}
	op.receipt = r
	return nil
}

func (s *Store) ReadRequestDecisionOperation(ctx context.Context, a ea.Access, id, operation string) (connection.DecisionOperationReceipt, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if !connection.ValidDecisionID(id) || !connection.ValidDecisionID(operation) {
		return connection.DecisionOperationReceipt{}, connection.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.DecisionOperationReceipt{}, connection.ErrOperationUnavailable
	}
	defer tx.Rollback(context.Background())
	r, e := readRequestDecisionOperationInTx(ctx, tx, a.Actor.ID, id, operation)
	if e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return connection.DecisionOperationReceipt{}, connection.ErrOperationUnavailable
	}
	return r, nil
}

func readRequestDecisionOperationInTx(ctx context.Context, tx pgx.Tx, owner, id, operation string) (connection.DecisionOperationReceipt, error) {
	if e := messageAuthenticateCurrent(ctx, tx, owner); e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	var sender, recipient string
	e := tx.QueryRow(ctx, `SELECT sender_account_id,recipient_account_id FROM connection_requests WHERE id=$1 AND (sender_account_id=$2 OR recipient_account_id=$2)`, id, owner).Scan(&sender, &recipient)
	if errors.Is(e, pgx.ErrNoRows) {
		return connection.DecisionOperationReceipt{}, connection.ErrNotFound
	}
	if e != nil {
		return connection.DecisionOperationReceipt{}, connection.ErrOperationUnavailable
	}
	rc := ctx
	if owner == recipient {
		rc = context.WithValue(ctx, messageContextKey{}, (*messageCurrent)(nil))
	}
	f, e := startMessageRoute(rc, tx, sender, recipient)
	if e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if e = messageLockCurrent(ctx, tx, owner); e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	r, e := readConnectionDecisionReceipt(ctx, tx, owner, operation)
	if e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if (r.Action == "withdraw" && owner != sender) || (r.Action != "withdraw" && owner != recipient) || r.RequestID != id {
		return connection.DecisionOperationReceipt{}, connection.ErrNotFound
	}
	// The record is historical. A current deny still prevents its disclosure.
	if f.blocked || (r.Action == "accept" && f.route == "BLOCK") {
		return connection.DecisionOperationReceipt{}, connection.ErrForbidden
	}
	if e = connection.ValidateDecisionReceipt(r, owner, id, operation, ""); e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if e = finishMessageRouteForActor(ctx, tx, f, owner); e != nil {
		return connection.DecisionOperationReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.DecisionOperationReceipt{}, connection.ErrOperationUnavailable
	}
	return r, nil
}

// Reuses the original read authority and final current-clock transaction. This
// neither retries a decision nor grants current relationship/chat permission.
func (s *Store) ValidateRequestDecisionOperationResponse(ctx context.Context, a ea.Access, receipt connection.DecisionOperationReceipt) error {
	if connection.ValidateDecisionReceipt(receipt, a.Actor.ID, receipt.RequestID, receipt.OperationID, receipt.Action) != nil {
		return connection.ErrOperationUnavailable
	}
	current, err := s.ReadRequestDecisionOperation(ctx, a, receipt.RequestID, receipt.OperationID)
	if err != nil {
		return err
	}
	current.RecordedAt = current.RecordedAt.UTC()
	receipt.RecordedAt = receipt.RecordedAt.UTC()
	if current != receipt {
		return connection.ErrOperationChanged
	}
	return nil
}
