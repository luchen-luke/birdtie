package postgres

import (
	"context"
	"errors"
	"reflect"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
)

var _ ap.HumanOperationStore = (*Store)(nil)

const participationOperationColumns = `owner_account_id,activity_id,operation_id,operation,effect_key,request_digest,tool_version,status,recorded_at,coalesce(effect_id::text,''),coalesce(state,''),effect_at,coalesce(reason,'')`
const participationOperationLock = `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.human-rsvp-operation.v1:'||$1::text||':'||$2::text,0))`

type humanParticipationWrite struct {
	input   ap.OperationInput
	digest  string
	receipt ap.OperationReceipt
	fresh   bool
}

func scanParticipationOperation(row pgx.Row) (ap.OperationReceipt, error) {
	r := ap.OperationReceipt{SchemaVersion: ap.HumanParticipationOperationSchema}
	e := row.Scan(&r.OwnerID, &r.ActivityID, &r.OperationID, &r.Operation, &r.EffectKey, &r.RequestDigest, &r.ToolVersion, &r.Status, &r.RecordedAt, &r.EffectID, &r.State, &r.EffectAt, &r.Reason)
	r.RecordedAt = r.RecordedAt.UTC()
	if r.EffectAt != nil {
		t := r.EffectAt.UTC()
		r.EffectAt = &t
	}
	return r, e
}
func readParticipationOperation(ctx context.Context, tx pgx.Tx, owner, op string) (ap.OperationReceipt, error) {
	return scanParticipationOperation(tx.QueryRow(ctx, `SELECT `+participationOperationColumns+` FROM human_participation_operation_receipts WHERE owner_account_id=$1 AND operation_id=$2`, owner, op))
}
func (s *Store) beginParticipationOperation(ctx context.Context, tx pgx.Tx, a ea.Access, activity string, w *humanParticipationWrite) (bool, error) {
	if _, e := tx.Exec(ctx, participationOperationLock, a.Actor.ID, w.input.OperationID); e != nil {
		return false, ea.ErrUnavailable
	}
	r, e := readParticipationOperation(ctx, tx, a.Actor.ID, w.input.OperationID)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, ea.ErrUnavailable
	}
	if !ap.ValidOperationReceipt(r, a.Actor.ID, r.ActivityID, w.input.OperationID) {
		return false, ea.ErrUnavailable
	}
	if r.ActivityID != activity || r.Operation != w.input.Operation || r.RequestDigest != w.digest {
		return false, ap.ErrOperationConflict
	}
	w.receipt = r
	// The native current Session check follows the operation lock wait. An old
	// approval is unnecessary for replay and never becomes a new authorization.
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return false, e
	}
	return true, nil
}
func recordParticipationOperation(ctx context.Context, tx pgx.Tx, a ea.Access, activity string, w *humanParticipationWrite, p ap.Participation) error {
	r, e := scanParticipationOperation(tx.QueryRow(ctx, `INSERT INTO human_participation_operation_receipts(owner_account_id,activity_id,operation_id,operation,effect_key,request_digest,tool_version,status,recorded_at,effect_id,state,effect_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'COMMITTED',clock_timestamp(),$8,$9,$10) RETURNING `+participationOperationColumns, a.Actor.ID, activity, w.input.OperationID, w.input.Operation, ap.HumanParticipationEffectKey(a.Actor.ID, w.input.OperationID), w.digest, ap.HumanParticipationToolVersion, p.ID, p.Status, p.UpdatedAt))
	if e != nil || !ap.ValidOperationReceipt(r, a.Actor.ID, activity, w.input.OperationID) {
		return ea.ErrUnavailable
	}
	w.receipt = r
	w.fresh = true
	return nil
}

func (s *Store) WriteHumanParticipationOperation(ctx context.Context, a ea.Access, activity string, in ap.OperationInput, b ea.BoundCondition) (ap.OperationReceipt, bool, error) {
	d, e := ap.HumanParticipationRequestDigest(a.Actor.ID, activity, in, b)
	if e != nil || !a.Valid() || a.Public {
		return ap.OperationReceipt{}, false, ea.ErrInvalid
	}
	w := &humanParticipationWrite{input: in, digest: d}
	if in.Operation == "JOIN" {
		_, _, e = s.joinActivity(ctx, a.Actor.ID, activity, &a, &b, w)
	} else {
		_, e = s.cancelParticipationBound(ctx, a, activity, b, w)
	}
	if e != nil {
		return ap.OperationReceipt{}, false, e
	}
	// The result is the immutable record captured in the original transaction.
	// HTTP records a fresh original conversion before its complete native output
	// validator; a revoked output cannot retroactively erase that real effect.
	// Never read the mutable participation to reconstruct old causal history.
	return w.receipt, w.fresh, nil
}

func (s *Store) participationOperationRead(ctx context.Context, a ea.Access, activity, op string, opaque bool) (ap.OperationReceipt, error) {
	if !a.Valid() || a.Public || !ap.ValidOperationID(activity) || !ap.ValidOperationID(op) {
		return ap.OperationReceipt{}, ea.ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ap.OperationReceipt{}, ea.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = s.lockEntityActionWriter(ctx, tx, a); e != nil {
		return ap.OperationReceipt{}, e
	}
	r, e := readParticipationOperation(ctx, tx, a.Actor.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return ap.OperationReceipt{}, ea.ErrNotFound
	}
	if e != nil {
		return ap.OperationReceipt{}, ea.ErrUnavailable
	}
	if r.ActivityID != activity {
		return ap.OperationReceipt{}, ap.ErrOperationConflict
	}
	if !ap.ValidOperationReceipt(r, a.Actor.ID, activity, op) {
		return ap.OperationReceipt{}, ea.ErrUnavailable
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return ap.OperationReceipt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ap.OperationReceipt{}, ea.ErrUnavailable
	}
	if !opaque && r.Status == "COMMITTED" {
		// Immutable receipt metadata no longer needs its pool lease. Release it
		// before acquiring the original source read transaction, including when
		// the pool has a single connection or is saturated by concurrent reads.
		source, e := s.ReadEntityActions(ctx, a, ea.Ref{Type: "activity", ID: activity})
		if e != nil {
			return ap.OperationReceipt{}, e
		}
		if e = s.RevalidateEntityActions(ctx, a, ea.Ref{Type: "activity", ID: activity}, source); e != nil {
			return ap.OperationReceipt{}, e
		}
	}
	if ctx.Err() != nil {
		return ap.OperationReceipt{}, ea.ErrUnavailable
	}
	return r, nil
}
func (s *Store) ReadHumanParticipationOperation(ctx context.Context, a ea.Access, activity, op string) (ap.OperationReceipt, error) {
	return s.participationOperationRead(ctx, a, activity, op, false)
}
func (s *Store) ReadHumanParticipationClosure(ctx context.Context, a ea.Access, activity, op string) (ap.OperationClosure, error) {
	r, e := s.participationOperationRead(ctx, a, activity, op, true)
	if e != nil {
		return ap.OperationClosure{}, e
	}
	return ap.NewOperationClosure(r), nil
}
func (s *Store) CloseHumanParticipationOperation(ctx context.Context, a ea.Access, activity, op string, in ap.OperationCloseInput) (ap.OperationClosure, error) {
	if !a.Valid() || a.Public || !ap.ValidOperationID(activity) || !ap.ValidOperationID(op) || !ap.ValidParticipationOperation(in.Operation) {
		return ap.OperationClosure{}, ea.ErrInvalid
	}
	// Validate the same strict digest boundary as the registered decoder.
	if _, e := ap.DecodeOperationClose([]byte(`{"operation":"` + in.Operation + `","requestDigest":"` + in.RequestDigest + `"}`)); e != nil {
		return ap.OperationClosure{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ap.OperationClosure{}, ea.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = s.lockEntityActionWriter(ctx, tx, a); e != nil {
		return ap.OperationClosure{}, e
	}
	if _, e = tx.Exec(ctx, participationOperationLock, a.Actor.ID, op); e != nil {
		return ap.OperationClosure{}, ea.ErrUnavailable
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return ap.OperationClosure{}, e
	}
	r, e := readParticipationOperation(ctx, tx, a.Actor.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = insertDomainAudit(ctx, tx, a.Actor.ID, "close", "activity_participation_operation", op, "human_rsvp_operation_close", &activity); e != nil {
			return ap.OperationClosure{}, ea.ErrUnavailable
		}
		r, e = scanParticipationOperation(tx.QueryRow(ctx, `INSERT INTO human_participation_operation_receipts(owner_account_id,activity_id,operation_id,operation,effect_key,request_digest,tool_version,status,reason,recorded_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'NO_EFFECT','USER_CLOSED',clock_timestamp()) RETURNING `+participationOperationColumns, a.Actor.ID, activity, op, in.Operation, ap.HumanParticipationEffectKey(a.Actor.ID, op), in.RequestDigest, ap.HumanParticipationToolVersion))
	}
	if e != nil {
		return ap.OperationClosure{}, ea.ErrUnavailable
	}
	if !ap.ValidOperationReceipt(r, a.Actor.ID, r.ActivityID, op) {
		return ap.OperationClosure{}, ea.ErrUnavailable
	}
	if r.ActivityID != activity || r.Operation != in.Operation || r.RequestDigest != in.RequestDigest {
		return ap.OperationClosure{}, ap.ErrOperationConflict
	}
	// No source grant, current RSVP mutation or renewal of the old approval.
	// This native clock is after all operation/audit/receipt trigger waits.
	if e = s.finishHumanMomentTx(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return ap.OperationClosure{}, e
	}
	if ctx.Err() != nil {
		return ap.OperationClosure{}, ea.ErrUnavailable
	}
	return ap.NewOperationClosure(r), nil
}
func (s *Store) ValidateHumanParticipationOperationResponse(ctx context.Context, a ea.Access, expected ap.OperationReceipt) error {
	if !ap.ValidOperationReceipt(expected, a.Actor.ID, expected.ActivityID, expected.OperationID) {
		return ea.ErrUnavailable
	}
	r, e := s.ReadHumanParticipationOperation(ctx, a, expected.ActivityID, expected.OperationID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(r, expected) {
		return ap.ErrOperationConflict
	}
	return ctx.Err()
}
func (s *Store) ValidateHumanParticipationClosureResponse(ctx context.Context, a ea.Access, expected ap.OperationClosure) error {
	if !ap.ValidOperationClosure(expected, a.Actor.ID, expected.ActivityID, expected.OperationID) {
		return ea.ErrUnavailable
	}
	r, e := s.ReadHumanParticipationClosure(ctx, a, expected.ActivityID, expected.OperationID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(r, expected) {
		return ap.ErrOperationConflict
	}
	return ctx.Err()
}
