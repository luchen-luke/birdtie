package postgres

import (
	"context"
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	si "github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ si.CreationGateway = (*Store)(nil)

func creationAccess(a si.CreationAccess) ai.Access {
	return ai.Access{Actor: a.Actor, SessionDigest: a.SessionDigest}
}
func creationError(e error) error {
	switch {
	case errors.Is(e, ai.ErrDenied):
		return si.ErrCreationDenied
	case errors.Is(e, ai.ErrConflict):
		return si.ErrCreationConflict
	case errors.Is(e, ai.ErrNotFound) || errors.Is(e, pgx.ErrNoRows):
		return si.ErrNotFound
	default:
		return si.ErrCreationUnavailable
	}
}
func creationReadInTx(ctx context.Context, tx pgx.Tx, g *humanActiveIntents, a si.CreationAccess, operation string) (si.CreationReceipt, error) {
	var r si.CreationReceipt
	r.SchemaVersion = si.CreationSchema
	e := tx.QueryRow(ctx, `SELECT owner_account_id,operation_id,request_digest,coalesce(source_task_id::text,''),status,coalesce(intent_id::text,''),coalesce(prior_intent_id::text,''),coalesce(reason,''),recorded_at FROM social_intent_creation_receipts WHERE owner_account_id=$1 AND operation_id=$2`, a.Actor.ID, operation).Scan(&r.OwnerID, &r.OperationID, &r.RequestDigest, &r.SourceTaskID, &r.Status, &r.IntentID, &r.PriorIntentID, &r.Reason, &r.RecordedAt)
	if e != nil {
		return r, creationError(e)
	}
	id := r.IntentID
	if id == "" {
		id = r.PriorIntentID
	}
	if id != "" {
		c, e := g.capture(ctx, tx, creationAccess(a), id)
		if e != nil {
			return r, creationError(e)
		}
		r.Intent = &c.Item.Intent
	}
	if e = si.ValidateCreationReceipt(r, a.Actor.ID, operation); e != nil {
		return r, e
	}
	return r, nil
}
func creationPersist(ctx context.Context, tx pgx.Tx, r *si.CreationReceipt) error {
	_, e := tx.Exec(ctx, `INSERT INTO social_intent_creation_receipts(owner_account_id,operation_id,request_digest,source_task_id,status,intent_id,prior_intent_id,reason) VALUES($1,$2,$3,nullif($4,'')::uuid,$5,nullif($6,'')::uuid,nullif($7,'')::uuid,nullif($8,''))`, r.OwnerID, r.OperationID, r.RequestDigest, r.SourceTaskID, r.Status, r.IntentID, r.PriorIntentID, r.Reason)
	if e != nil {
		return si.ErrCreationUnavailable
	}
	return tx.QueryRow(ctx, `SELECT recorded_at FROM social_intent_creation_receipts WHERE owner_account_id=$1 AND operation_id=$2`, r.OwnerID, r.OperationID).Scan(&r.RecordedAt)
}
func (s *Store) CreatePrivateDraft(ctx context.Context, a si.CreationAccess, source string, in si.DraftInput) (si.CreationReceipt, bool, error) {
	var empty si.CreationReceipt
	n, e := si.NormalizeCreation(in, source)
	if e != nil {
		return empty, false, e
	}
	digest, e := si.CreationDigest(a.Actor.ID, source, n)
	if e != nil {
		return empty, false, e
	}
	g := &humanActiveIntents{store: s}
	tx, b, e := g.begin(ctx, creationAccess(a), true)
	if e != nil {
		return empty, false, creationError(e)
	}
	defer tx.Rollback(context.Background())
	// The advisory lock is owner/domain/operation scoped and held in this actual
	// transaction. No in-process mutex or alternate ledger provides authority.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('social-intent-create:'||$1||':'||$2,0))`, a.Actor.ID, n.OperationID); e != nil {
		return empty, false, si.ErrCreationUnavailable
	}
	old, e := creationReadInTx(ctx, tx, g, a, n.OperationID)
	if e == nil {
		if old.RequestDigest != digest || old.SourceTaskID != source {
			return empty, false, si.ErrCreationConflict
		}
		if e = g.finish(ctx, tx, creationAccess(a), &b); e != nil {
			return empty, false, creationError(e)
		}
		if e = tx.Commit(ctx); e != nil {
			return empty, false, si.ErrCreationUnavailable
		}
		return old, false, nil
	}
	if !errors.Is(e, si.ErrNotFound) {
		return empty, false, e
	}
	r := si.CreationReceipt{SchemaVersion: si.CreationSchema, OwnerID: a.Actor.ID, OperationID: n.OperationID, RequestDigest: digest, SourceTaskID: source, Status: "NO_EFFECT"}
	finishNoEffect := func(reason, prior string) (si.CreationReceipt, bool, error) {
		r.Status = "NO_EFFECT"
		r.IntentID = ""
		r.PriorIntentID = prior
		r.Reason = reason
		r.Intent = nil
		if prior != "" {
			c, e := g.capture(ctx, tx, creationAccess(a), prior)
			if e != nil {
				return empty, false, creationError(e)
			}
			r.Intent = &c.Item.Intent
		}
		if e = creationPersist(ctx, tx, &r); e != nil {
			return empty, false, e
		}
		if e = g.finish(ctx, tx, creationAccess(a), &b); e != nil {
			return empty, false, creationError(e)
		}
		if e = tx.Commit(ctx); e != nil {
			return empty, false, si.ErrCreationUnavailable
		}
		return r, false, nil
	}
	if source != "" {
		var task string
		e = tx.QueryRow(ctx, `SELECT id FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND acting_user_account_id=$2 AND principal_type='person' AND intent='FIND_ACTIVITY' AND status='COMPLETED' FOR SHARE`, source, a.Actor.ID).Scan(&task)
		if errors.Is(e, pgx.ErrNoRows) {
			return finishNoEffect("SOURCE_UNAVAILABLE", "")
		}
		if e != nil {
			return empty, false, si.ErrCreationUnavailable
		}
		var prior string
		e = tx.QueryRow(ctx, `SELECT id FROM social_intents WHERE source_agent_task_id=$1 AND creator_account_id=$2 FOR SHARE`, source, a.Actor.ID).Scan(&prior)
		if e == nil {
			return finishNoEffect("SOURCE_ALREADY_EXISTS", prior)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return empty, false, si.ErrCreationUnavailable
		}
	}
	if e = g.authority(ctx, tx, creationAccess(a), &b); e != nil {
		return empty, false, creationError(e)
	}
	if !n.ExpiresAt.After(b.now.Add(time.Minute)) || n.ExpiresAt.After(b.now.Add(90*24*time.Hour)) {
		return finishNoEffect("EXPIRED", "")
	}
	frame, live, _, _, deadline, e := g.sources(ctx, tx, creationAccess(a), "", n, false)
	if e != nil {
		return empty, false, creationError(e)
	}
	if !live {
		return finishNoEffect("SOURCE_UNAVAILABLE", "")
	}
	if n.ExpiresAt.Before(deadline) {
		deadline = n.ExpiresAt
	}
	if _, e = tx.Exec(ctx, `SAVEPOINT social_intent_creation_effect`); e != nil {
		return empty, false, si.ErrCreationUnavailable
	}
	item, e := s.createSocialIntentDraftInTx(ctx, tx, a.Actor.ID, source, n)
	if errors.Is(e, si.ErrConflict) {
		if _, e = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT social_intent_creation_effect`); e != nil {
			return empty, false, si.ErrCreationUnavailable
		}
		var prior string
		if e = tx.QueryRow(ctx, `SELECT id FROM social_intents WHERE source_agent_task_id=$1 AND creator_account_id=$2 FOR SHARE`, source, a.Actor.ID).Scan(&prior); e != nil {
			return empty, false, creationError(e)
		}
		return finishNoEffect("SOURCE_ALREADY_EXISTS", prior)
	}
	if e != nil {
		return empty, false, creationError(e)
	}
	r.Status = "COMMITTED"
	r.IntentID = item.ID
	r.Intent = &item
	if e = creationPersist(ctx, tx, &r); e != nil {
		return empty, false, e
	}
	if e = g.finish(ctx, tx, creationAccess(a), &b); e != nil {
		return empty, false, creationError(e)
	}
	// One native statement checks identity, exact current sources and deadline
	// after all waits in the insert, audit and durable receipt paths.
	e = g.allCurrent(ctx, tx, creationAccess(a), &b, []activeExpectation{{Draft: n, Source: frame, Deadline: deadline}})
	if e != nil {
		if !errors.Is(e, ai.ErrConflict) {
			return empty, false, creationError(e)
		}
		if _, e = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT social_intent_creation_effect`); e != nil {
			return empty, false, si.ErrCreationUnavailable
		}
		return finishNoEffect("SOURCE_CHANGED", "")
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, false, si.ErrCreationUnavailable
	}
	return r, true, nil
}
func (s *Store) ReadCreation(ctx context.Context, a si.CreationAccess, operation string) (si.CreationReceipt, error) {
	if !ai.UUID(operation) {
		return si.CreationReceipt{}, si.ErrCreationInvalid
	}
	g := &humanActiveIntents{store: s}
	tx, b, e := g.begin(ctx, creationAccess(a), false)
	if e != nil {
		return si.CreationReceipt{}, creationError(e)
	}
	defer tx.Rollback(context.Background())
	r, e := creationReadInTx(ctx, tx, g, a, operation)
	if e != nil {
		return r, e
	}
	if e = g.finish(ctx, tx, creationAccess(a), &b); e != nil {
		return r, creationError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return r, si.ErrCreationUnavailable
	}
	return r, nil
}
func (s *Store) ReadTaskDraft(ctx context.Context, a si.CreationAccess, source string) (si.TaskDraftReceipt, error) {
	var r si.TaskDraftReceipt
	if !ai.UUID(source) {
		return r, si.ErrCreationInvalid
	}
	g := &humanActiveIntents{store: s}
	tx, b, e := g.begin(ctx, creationAccess(a), false)
	if e != nil {
		return r, creationError(e)
	}
	defer tx.Rollback(context.Background())
	var id string
	e = tx.QueryRow(ctx, `SELECT i.id FROM social_intents i JOIN agent_tasks t ON t.id=i.source_agent_task_id WHERE i.source_agent_task_id=$1 AND i.creator_account_id=$2 AND t.owner_account_id=$2 AND t.acting_user_account_id=$2 AND t.principal_type='person' FOR SHARE OF i,t`, source, a.Actor.ID).Scan(&id)
	if e != nil {
		return r, creationError(e)
	}
	c, e := g.capture(ctx, tx, creationAccess(a), id)
	if e != nil {
		return r, creationError(e)
	}
	r = si.TaskDraftReceipt{SchemaVersion: si.CreationSchema, OwnerID: a.Actor.ID, SourceTaskID: source, IntentID: id, Intent: c.Item.Intent}
	if e = g.finish(ctx, tx, creationAccess(a), &b); e != nil {
		return r, creationError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return r, si.ErrCreationUnavailable
	}
	return r, nil
}
