package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/jackc/pgx/v5"
)

// Metadata inputs select original approved records; none of these IDs mint
// consent. The second operation exists before its public-source wire is known.
type LiveSourceAnswerRunInput struct {
	RunID                               string
	Search                              modelegressbudget.ReserveInput
	ModelOperationID, ModelPriceVersion string
	MaxOutputTokens                     int
}

// This association cannot be supplied by HTTP/JSON, stored metadata or a
// rollout flag. Every use validates its actual original Run under native locks.
type nativeLiveSourceRunAssociation struct {
	handle    *nativeLiveSourceAnswerRun
	operation string
	fence     int64
}

// Fixed internal stage names locate failed native checks without putting task,
// source or credential data into errors. The original error identity remains.
type nativeLiveRunStageError struct {
	stage string
	err   error
}

func (e *nativeLiveRunStageError) Error() string {
	return "native live Run " + e.stage + ": " + nativeLiveFailureSummary(e.err)
}
func (e *nativeLiveRunStageError) Unwrap() error { return e.err }
func nativeLiveStage(stage string, e error) error {
	if e == nil {
		return nil
	}
	return &nativeLiveRunStageError{stage: stage, err: e}
}

// Fixed classifications are safe for server logs. Unwrap retains the original
// cause for errors.Is/As; arbitrary DB, transport and provider text is never
// formatted here (URL errors can contain query strings or credentials).
func nativeLiveFailureSummary(err error) string {
	if staged, ok := err.(*nativeLiveRunStageError); ok {
		return staged.Error()
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]string, 0, len(joined.Unwrap()))
		for _, child := range joined.Unwrap() {
			parts = append(parts, nativeLiveFailureSummary(child))
		}
		return strings.Join(parts, "; ")
	}
	var provider agenttool.TencentWSAProviderError
	if errors.As(err, &provider) {
		switch provider.Code {
		case "TRANSPORT_UNKNOWN", "HTTP_UNKNOWN", "AUTHENTICATION", "RATE_LIMIT", "TEMPORARY", "INVALID_REQUEST", "PROVIDER_UNKNOWN", "RESOURCE_UNAVAILABLE", "QUERY_REJECTED":
			return "WSA_" + provider.Code
		default:
			return "WSA_PROVIDER_UNKNOWN"
		}
	}
	for _, classified := range []struct {
		cause error
		code  string
	}{
		{context.Canceled, "CANCELED"}, {context.DeadlineExceeded, "DEADLINE"},
		{modelegressbudget.ErrDenied, "DENIED"}, {modelegressbudget.ErrConflict, "CONFLICT"},
		{modelegressbudget.ErrBudget, "BUDGET"}, {modelegressbudget.ErrInvalid, "INVALID"},
		{modelegressbudget.ErrUnavailable, "UNAVAILABLE"}, {agenttool.ErrTencentWSARequest, "WSA_REQUEST"},
		{agenttool.ErrTencentWSAResponse, "WSA_RESPONSE"}, {agenttool.ErrTencentWSAConfig, "WSA_CONFIG"},
	} {
		if errors.Is(err, classified.cause) {
			return classified.code
		}
	}
	return "NATIVE_ERROR"
}

type nativeLiveSourceAnswerRun struct {
	mu                             sync.Mutex
	store                          *Store
	access                         agentevent.Access
	controller                     *agentfeature.Controller
	ticket                         agentfeature.Ticket
	gate                           modelgateway.LiveGate
	projector                      LiveWireProjector
	id, owner, session, generation string
	fence                          int64
	search                         modelegressbudget.ReserveInput
	original                       PreparedLiveEgress
	modelOperation                 string
	modelInput                     modelegressbudget.PreviewInput
	deadline                       time.Time
	checkpoint                     modelegressbudget.LocalReleaseCheckpoint
	searchUsed                     atomic.Bool
	modelUsed                      atomic.Bool
	batch                          OwnLiveSourceBatch
	completedResult                *modelgateway.Result
	lastNativeFailure              atomic.Pointer[nativeLiveRunStageError]
}

func (*nativeLiveSourceAnswerRun) MarshalJSON() ([]byte, error) {
	return nil, modelegressbudget.ErrServerOnly
}
func (*nativeLiveSourceAnswerRun) UnmarshalJSON([]byte) error { return modelegressbudget.ErrServerOnly }
func (*nativeLiveSourceAnswerRun) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "nativeLiveSourceAnswerRun{redacted}")
}
func (*nativeLiveSourceRunAssociation) MarshalJSON() ([]byte, error) {
	return nil, modelegressbudget.ErrServerOnly
}
func (*nativeLiveSourceRunAssociation) UnmarshalJSON([]byte) error {
	return modelegressbudget.ErrServerOnly
}

func validateLiveSourceRunAssociationTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, association *nativeLiveSourceRunAssociation, b OwnLiveSourceBatch) error {
	if association == nil || association.handle == nil {
		return modelegressbudget.ErrDenied
	}
	h := association.handle
	if ctx == nil || ctx.Err() != nil || h.store == nil || h.access != a || !b.valid || b.access != a || association.fence != h.fence || association.operation != h.search.OperationID || b.data.Evidence.OperationID != h.search.OperationID || b.owner != h.owner || b.session != h.session || b.root != h.search.RootTraceID || b.task != h.search.TaskID || b.binding != h.original.binding || b.source != h.original.source || b.authority != h.original.authority || !h.brake(ctx) {
		return modelegressbudget.ErrDenied
	}
	r, e := readModelRun(ctx, tx, h.id, h.owner)
	if e != nil {
		return e
	}
	if !h.matchesRun(r, false) || len(r.control.Steps) != 2 || r.control.Steps[0].OperationID != association.operation || r.control.Steps[0].Kind != modelrequestrun.SourceRetrieval || r.control.Steps[0].State != modelrequestrun.StepUnknown || r.control.Steps[0].PreviewID != b.data.Evidence.PreviewID || r.control.Steps[0].RequestDigest != b.data.Evidence.SourceRequestDigest || b.data.Evidence.DeadlineAt.Before(r.control.DeadlineAt) {
		return modelegressbudget.ErrDenied
	}
	return egressFinish(ctx, tx, h.session, nil, r.control.DeadlineAt, r.control.LeaseUntil)
}

func (h *nativeLiveSourceAnswerRun) association() *nativeLiveSourceRunAssociation {
	return &nativeLiveSourceRunAssociation{handle: h, operation: h.search.OperationID, fence: h.fence}
}
func (h *nativeLiveSourceAnswerRun) brake(ctx context.Context) bool {
	return h != nil && ctx != nil && ctx.Err() == nil && h.controller != nil && h.controller.Current(h.ticket) && nativeLiveGatePresent(h.gate) && h.gate.InferenceEnabled(ctx) && ctx.Err() == nil && h.deadline.After(time.Now())
}
func (h *nativeLiveSourceAnswerRun) matchesRun(r storedModelRun, finished bool) bool {
	return r.kind == modelrequestrun.LiveSourceAnswer && r.control.ModelRunID == h.id && r.control.Owner.ID == h.owner && r.session == h.session && r.agent == h.original.request.Agent.AgentID && r.generation == h.generation && r.source == h.original.source && r.authority == h.original.authority && r.control.Fence == h.fence && r.control.RootTraceID == h.search.RootTraceID && r.control.TaskID == h.search.TaskID && r.control.BindingID == h.original.binding && !r.control.DeadlineAt.After(h.deadline) && (r.control.State == modelrequestrun.RunPlanned || r.control.State == modelrequestrun.RunRunning || (finished && r.control.State == modelrequestrun.RunFinished))
}

func (s *Store) CreateOwnLiveSourceAnswerRun(ctx context.Context, a agentevent.Access, in LiveSourceAnswerRunInput, c *agentfeature.Controller, ticket agentfeature.Ticket, projector LiveWireProjector, gate modelgateway.LiveGate) (*nativeLiveSourceAnswerRun, modelrequestrun.Control, error) {
	var empty modelrequestrun.Control
	if ctx == nil || ctx.Err() != nil || !egressUUID(in.RunID) || !egressUUID(in.Search.OperationID) || !egressUUID(in.Search.PreviewID) || !egressUUID(in.ModelOperationID) || in.ModelOperationID == in.Search.OperationID || in.MaxOutputTokens < 1 || in.MaxOutputTokens > modelgateway.TencentLiveMaxOutputTokens || c == nil || !c.Current(ticket) || !liveProjectorPresent(projector) || !nativeLiveGatePresent(gate) || !gate.InferenceEnabled(ctx) {
		return nil, empty, modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return nil, empty, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return nil, empty, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return nil, empty, e
	}
	ops := []string{in.Search.OperationID, in.ModelOperationID}
	sort.Strings(ops)
	for _, op := range ops {
		if e = modelOperationLock(ctx, tx, op); e != nil {
			return nil, empty, e
		}
		var present bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_budget_reservations WHERE operation_id=$1) OR EXISTS(SELECT 1 FROM model_request_run_steps WHERE operation_id=$1)`, op).Scan(&present); e != nil {
			return nil, empty, egressError(e)
		}
		if present {
			return nil, empty, modelegressbudget.ErrConflict
		}
	}
	v, e := readLiveEgressPreview(ctx, tx, in.Search.PreviewID)
	if e != nil {
		return nil, empty, e
	}
	if v.status != "APPROVED" || v.kind != modelegressbudget.LiveCall || !liveStandaloneCallScope(v.scope) || v.owner != owner || v.session != session || v.in.TaskID != in.Search.TaskID || v.in.RootTraceID != in.Search.RootTraceID {
		return nil, empty, modelegressbudget.ErrDenied
	}
	p, e := s.prepareStandaloneLivePreviewTx(ctx, tx, a, v, nil)
	if e != nil {
		return nil, empty, e
	}
	if !matchLivePreview(v, p) || in.RunID == p.binding {
		return nil, empty, modelegressbudget.ErrDenied
	}
	price, e := readLivePrice(ctx, tx, in.ModelPriceVersion)
	if e != nil {
		return nil, empty, e
	}
	upper, e := modelegressbudget.BoundLive(price, in.MaxOutputTokens, time.Now())
	if e != nil || price.Kind != modelegressbudget.LiveToken || upper.Amount.CostMicros > liveRequestCashLimit {
		return nil, empty, modelegressbudget.ErrBudget
	}
	deadline := retryMinimum(p.input.DeadlineAt, price.Base.ExpiresAt)
	if d, ok := ctx.Deadline(); ok {
		deadline = retryMinimum(deadline, d)
	}
	cp, e := captureLocalAttemptCheckpoint(ctx, tx, session, in.Search.OperationID, p.request, deadline, deadline, c, ticket)
	if e != nil {
		return nil, empty, e
	}
	// Persist and compare one canonical PostgreSQL instant. Context deadlines
	// may carry monotonic data and sub-microsecond precision; rounding down
	// removes those representations without extending the original deadline.
	deadline = retryMinimum(deadline, cp.ValidUntil()).UTC().Truncate(time.Microsecond)
	var generation string
	if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, in.Search.TaskID).Scan(&generation); e != nil {
		return nil, empty, egressError(e)
	}
	h := &nativeLiveSourceAnswerRun{store: s, access: a, controller: c, ticket: ticket, gate: gate, projector: projector, id: in.RunID, owner: owner, session: session, generation: generation, fence: 1, search: in.Search, original: p, modelOperation: in.ModelOperationID, modelInput: modelegressbudget.PreviewInput{RootTraceID: in.Search.RootTraceID, TaskID: in.Search.TaskID, PriceVersion: in.ModelPriceVersion, MaxOutputTokens: in.MaxOutputTokens, DeadlineAt: deadline}, deadline: deadline, checkpoint: cp}
	if !h.brake(ctx) {
		return nil, empty, modelegressbudget.ErrDenied
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_request_runs(id,owner_id,session_id,agent_id,root_trace_id,task_id,binding_id,task_generation,source_token,authority_token,state,deadline_at,lease_until,run_kind) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'PLANNED',$11,$11,'LIVE_SOURCE_ANSWER')`, h.id, owner, session, p.request.Agent.AgentID, in.Search.RootTraceID, in.Search.TaskID, p.binding, generation, p.source, p.authority, deadline)
	if e != nil {
		return nil, empty, egressError(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_request_run_steps(run_id,ordinal,operation_id,preview_id,price_version,request_digest,step_kind,binding_state,planned_max_output_tokens) VALUES($1,1,$2,$3,$4,$5,'SOURCE_RETRIEVAL','BOUND',0),($1,2,$6,NULL,$7,NULL,'MODEL_INFERENCE','WAITING_SOURCE',$8)`, h.id, in.Search.OperationID, in.Search.PreviewID, p.price.Base.Version, p.digest, in.ModelOperationID, in.ModelPriceVersion, in.MaxOutputTokens)
	if e != nil {
		return nil, empty, egressError(e)
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil {
		return nil, empty, e
	}
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return nil, empty, e
	}
	return h, modelrequestrun.Clone(r.control), nil
}

// Ordinary callers lock Session/account -> owner -> original Run/steps. No
// new ticket, consent, elapsed window or private prompt is reconstructed.
func (h *nativeLiveSourceAnswerRun) beginCurrent(ctx context.Context, finished bool) (pgx.Tx, storedModelRun, error) {
	var empty storedModelRun
	if !h.brake(ctx) || !h.checkpoint.ValidAt(time.Now()) {
		return nil, empty, modelegressbudget.ErrDenied
	}
	tx, e := h.store.beginEgress(ctx)
	if e != nil {
		return nil, empty, e
	}
	fail := func(e error) (pgx.Tx, storedModelRun, error) { tx.Rollback(ctx); return nil, empty, e }
	owner, session, e := h.store.egressOwner(ctx, tx, h.access)
	if e != nil {
		return fail(e)
	}
	if owner != h.owner || session != h.session {
		return fail(modelegressbudget.ErrDenied)
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return fail(e)
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil {
		return fail(e)
	}
	if !h.matchesRun(r, finished) {
		return fail(nativeLiveStage("current-run-snapshot", modelegressbudget.ErrDenied))
	}
	if e = h.currentRunTx(ctx, tx, r, finished); e != nil {
		return fail(e)
	}
	return tx, r, nil
}
func (h *nativeLiveSourceAnswerRun) currentRunTx(ctx context.Context, tx pgx.Tx, r storedModelRun, finished bool) error {
	if !h.matchesRun(r, finished) {
		return nativeLiveStage("current-run-snapshot", modelegressbudget.ErrDenied)
	}
	if !h.brake(ctx) {
		return nativeLiveStage("current-native-brake", modelegressbudget.ErrDenied)
	}
	if len(r.control.Steps) != 2 {
		return nativeLiveStage("current-step-shape", modelegressbudget.ErrDenied)
	}
	// Run timestamps come from PostgreSQL clock_timestamp(). Validate them
	// against that same native clock after the original locks and reads, while
	// the independent context/monotonic checkpoint still bounds release.
	var nativeNow time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&nativeNow); e != nil {
		return nativeLiveStage("current-control-clock", egressError(e))
	}
	if modelrequestrun.ValidateControl(r.control, nativeNow) != nil {
		return nativeLiveStage("current-control-db-clock", modelegressbudget.ErrDenied)
	}
	first := r.control.Steps[0]
	if first.OperationID != h.search.OperationID || first.PreviewID != h.search.PreviewID || first.RequestDigest != h.original.digest || first.PriceVersion != h.original.price.Base.Version || r.control.Steps[1].OperationID != h.modelOperation || r.control.Steps[1].PriceVersion != h.modelInput.PriceVersion || r.control.Steps[1].MaxOutputTokens != h.modelInput.MaxOutputTokens {
		return nativeLiveStage("current-planned-fields", modelegressbudget.ErrDenied)
	}
	v, e := readLiveEgressPreview(ctx, tx, first.PreviewID)
	if e != nil {
		return e
	}
	p, e := h.store.prepareStandaloneLivePreviewTx(ctx, tx, h.access, v, nil)
	if e != nil {
		return nativeLiveStage("current-call-prepare", e)
	}
	if v.status != "APPROVED" || !matchLivePreview(v, p) || p.digest != h.original.digest || !nativeLiveRequestEqual(p.Request(), h.original.Request()) {
		return nativeLiveStage("current-call-exact", modelegressbudget.ErrDenied)
	}
	var generation string
	if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, h.search.TaskID).Scan(&generation); e != nil {
		return egressError(e)
	}
	if generation != h.generation {
		return nativeLiveStage("current-task-generation", modelegressbudget.ErrDenied)
	}
	return egressFinish(ctx, tx, h.session, nil, r.control.DeadlineAt, r.control.LeaseUntil)
}
func (h *nativeLiveSourceAnswerRun) commitCurrent(ctx context.Context, tx pgx.Tx, r storedModelRun, finished bool) error {
	if e := h.currentRunTx(ctx, tx, r, finished); e != nil {
		return nativeLiveStage("commit-current", e)
	}
	cp, e := captureLocalAttemptCheckpoint(ctx, tx, h.session, h.search.OperationID, h.original.request, retryMinimum(r.control.DeadlineAt, r.control.LeaseUntil), h.original.price.Base.ExpiresAt, h.controller, h.ticket)
	if e != nil {
		return nativeLiveStage("commit-checkpoint", e)
	}
	if e = tx.Commit(ctx); e != nil {
		return egressError(e)
	}
	if cp.Remaining(time.Now()) < h.checkpoint.Remaining(time.Now()) {
		h.checkpoint = cp
	}
	if !h.checkpoint.ValidAt(time.Now()) || !h.brake(ctx) {
		return modelegressbudget.ErrDenied
	}
	return nil
}

func (h *nativeLiveSourceAnswerRun) preparedStepTx(ctx context.Context, tx pgx.Tx, r storedModelRun, ordinal int) (PreparedLiveEgress, error) {
	if ordinal < 1 || ordinal > 2 {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	step := r.control.Steps[ordinal-1]
	if step.BindingState != modelrequestrun.Bound {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	v, e := readLiveEgressPreview(ctx, tx, step.PreviewID)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	p, e := h.store.prepareStoredLiveEgressWithSourceRunTx(ctx, tx, h.access, v, h.projector, h.association())
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if v.status != "APPROVED" || v.owner != h.owner || v.session != h.session || !matchLivePreview(v, p) || p.digest != step.RequestDigest || p.price.Base.Version != step.PriceVersion {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	if ordinal == 1 && p.price.Kind != modelegressbudget.LiveCall {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	if ordinal == 2 && (p.price.Kind != modelegressbudget.LiveToken || v.sourceEvidenceDigest != step.SourceEvidenceDigest || p.sourceBatch.evidenceDigest != step.SourceEvidenceDigest || p.input != h.modelInput) {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	return p, nil
}
func (h *nativeLiveSourceAnswerRun) reserveStepLocked(ctx context.Context, ordinal int) (modelegressbudget.Reservation, error) {
	var empty modelegressbudget.Reservation
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	step := r.control.Steps[ordinal-1]
	if step.BindingState != modelrequestrun.Bound || (step.State != modelrequestrun.StepPlanned && step.State != modelrequestrun.StepReserved) {
		return empty, modelegressbudget.ErrConflict
	}
	if ordinal == 2 && r.control.Steps[0].State != modelrequestrun.StepUnknown {
		return empty, modelegressbudget.ErrDenied
	}
	for i, s := range r.control.Steps {
		if i != ordinal-1 && (s.State == modelrequestrun.StepReserved || s.State == modelrequestrun.StepInFlight) {
			return empty, modelegressbudget.ErrConflict
		}
	}
	in := modelegressbudget.ReserveInput{OperationID: step.OperationID, PreviewID: step.PreviewID, RootTraceID: h.search.RootTraceID, TaskID: h.search.TaskID}
	out, e := h.store.reserveLiveAttemptTx(ctx, tx, h.access, in, h.controller, h.ticket, h.owner, h.session, h.projector, h.association())
	if e != nil {
		return empty, e
	}
	if out.State != "RESERVED" {
		return empty, modelegressbudget.ErrConflict
	}
	if step.State == modelrequestrun.StepPlanned {
		if _, e = tx.Exec(ctx, `UPDATE model_request_run_steps SET state='RESERVED',reservation_id=operation_id,updated_at=clock_timestamp() WHERE run_id=$1 AND ordinal=$2`, h.id, ordinal); e != nil {
			return empty, egressError(e)
		}
		if _, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='RUNNING',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id); e != nil {
			return empty, egressError(e)
		}
	}
	r, e = readModelRun(ctx, tx, h.id, h.owner)
	if e != nil {
		return empty, e
	}
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return empty, e
	}
	return out, nil
}
func (h *nativeLiveSourceAnswerRun) beginStepLocked(ctx context.Context, ordinal int) (PreparedLiveEgress, error) {
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	defer tx.Rollback(ctx)
	step := r.control.Steps[ordinal-1]
	if step.State != modelrequestrun.StepReserved {
		return PreparedLiveEgress{}, modelegressbudget.ErrConflict
	}
	p, e := h.store.beginLiveAttemptTx(ctx, tx, h.access, step.OperationID, h.controller, h.ticket, h.owner, h.session, h.projector, h.association())
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE model_request_run_steps SET state='IN_FLIGHT',updated_at=clock_timestamp() WHERE run_id=$1 AND ordinal=$2`, h.id, ordinal); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE model_request_runs SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	r, e = readModelRun(ctx, tx, h.id, h.owner)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return PreparedLiveEgress{}, e
	}
	return p, nil
}
func (h *nativeLiveSourceAnswerRun) checkStep(ctx context.Context, ordinal int) (prepared PreparedLiveEgress, err error) {
	stage := "check-input"
	defer func() {
		err = nativeLiveStage(stage, err)
		if h != nil && err != nil {
			h.lastNativeFailure.Store(err.(*nativeLiveRunStageError))
		}
	}()
	if h == nil {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	stage = "check-current"
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	defer tx.Rollback(ctx)
	stage = "check-source-replay"
	p, e := h.preparedStepTx(ctx, tx, r, ordinal)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	stage = "check-reservation"
	reservation, kind, e := readLiveReservation(ctx, tx, r.control.Steps[ordinal-1].OperationID, h.owner)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if !liveCheckPhase(reservation) || string(r.control.Steps[ordinal-1].State) != reservation.State || reservation.RequestDigest != p.digest || kind != p.price.Kind || reservation.Upper != p.upper.Amount {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	stage = "check-budget"
	rows, e := readEgressBudgets(ctx, tx, h.owner, h.search.RootTraceID, h.search.TaskID, "CNY")
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	for _, row := range rows {
		if row.used.CostMicros > liveOwnerCashLimit || ((row.scope == "ROOT" || row.scope == "TASK") && (row.used.CostMicros > liveRootCashLimit || row.used.Requests > 2)) {
			return PreparedLiveEgress{}, modelegressbudget.ErrBudget
		}
	}
	stage = "check-commit"
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return PreparedLiveEgress{}, e
	}
	return p, nil
}

// Cleanup requires only the current original owner/session and exact linked
// operation. Source revocation, elapsed windows and retired fences cannot erase
// an attempted request. Full original cash/token allocation is never refunded.
func (h *nativeLiveSourceAnswerRun) settleUnknown(ctx context.Context, ordinal int, usage modelgateway.Usage) error {
	if h == nil || h.store == nil || ordinal < 1 || ordinal > 2 {
		return modelegressbudget.ErrDenied
	}
	tx, e := h.store.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := h.store.egressOwner(ctx, tx, h.access)
	if e != nil {
		return e
	}
	if owner != h.owner || session != h.session {
		return modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil {
		return e
	}
	if r.kind != modelrequestrun.LiveSourceAnswer || len(r.control.Steps) != 2 {
		return modelegressbudget.ErrDenied
	}
	step := r.control.Steps[ordinal-1]
	op := h.search.OperationID
	if ordinal == 2 {
		op = h.modelOperation
	}
	if step.OperationID != op || step.ReservationID != op {
		return modelegressbudget.ErrDenied
	}
	if e = modelOperationLock(ctx, tx, op); e != nil {
		return e
	}
	out, e := h.store.finishLiveAttemptTx(ctx, tx, op, usage, owner, session)
	if e != nil {
		return e
	}
	if e = syncModelRunAccounting(ctx, tx, &modelRunAccounting{run: r, ordinal: ordinal}, out); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

func (h *nativeLiveSourceAnswerRun) ReserveSource(ctx context.Context) (modelegressbudget.Reservation, error) {
	if h == nil {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reserveStepLocked(ctx, 1)
}

func (h *nativeLiveSourceAnswerRun) ExecuteSourceSearch(ctx context.Context, adapter *agenttool.TencentWSAAdapter) (batch OwnLiveSourceBatch, err error) {
	stage := "search-input"
	defer func() { err = nativeLiveStage(stage, err) }()
	if h == nil || adapter == nil || !h.searchUsed.CompareAndSwap(false, true) {
		return OwnLiveSourceBatch{}, modelegressbudget.ErrDenied
	}
	stage = "search-reserve"
	h.mu.Lock()
	_, e := h.reserveStepLocked(ctx, 1)
	var p PreparedLiveEgress
	if e == nil {
		stage = "search-begin"
		p, e = h.beginStepLocked(ctx, 1)
	}
	h.mu.Unlock()
	if e != nil {
		return OwnLiveSourceBatch{}, e
	} // No send on failed/uncertain commit.
	deadline := retryMinimum(h.deadline, time.Now().Add(agenttool.TencentWSAMaxDeadline))
	callctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var result agenttool.TencentWSAResult
	var guardFailure atomic.Pointer[nativeLiveRunStageError]
	stage = "search-before-call"
	if _, e = h.checkStep(callctx, 1); e == nil {
		stage = "search-provider"
		result, e = adapter.SearchWithNativeGuard(callctx, p.SearchQuery(), deadline, func(wirectx context.Context) error {
			current, err := h.checkStep(wirectx, 1)
			if err == nil && (current.Kind() != modelegressbudget.LiveCall || current.RequestDigest() != p.RequestDigest() || current.PayloadDigest() != p.PayloadDigest() || current.SearchQuery() != p.SearchQuery()) {
				err = nativeLiveStage("search-wire-exact", modelegressbudget.ErrDenied)
			}
			if err != nil {
				guardFailure.Store(&nativeLiveRunStageError{stage: "search-wire-current", err: err})
				return err
			}
			return nil
		})
	}
	_, currentErr := h.checkStep(callctx, 1)
	accountCtx, accountCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	accountErr := h.settleUnknown(accountCtx, 1, modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"})
	accountCancel()
	if e != nil || currentErr != nil || accountErr != nil || guardFailure.Load() != nil {
		failed := []error{nativeLiveStage(stage, e), nativeLiveStage("search-after-current", currentErr), nativeLiveStage("search-settle-unknown", accountErr)}
		if guard := guardFailure.Load(); guard != nil {
			failed = append(failed, guard)
		}
		stage = "search-call-result"
		return OwnLiveSourceBatch{}, errors.Join(failed...)
	}
	output := OwnLiveSearchOutput{result: result, query: p.SearchQuery(), prepared: p, operation: h.search.OperationID, access: h.access, observedAt: time.Now().UTC().Truncate(time.Microsecond)}
	h.mu.Lock()
	defer h.mu.Unlock()
	stage = "search-auth-current"
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	defer tx.Rollback(ctx)
	stage = "search-auth-batch"
	b, e := h.store.authenticateLiveSearchSourcesTx(ctx, tx, h.access, output, h.association())
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	stage = "search-auth-commit"
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return OwnLiveSourceBatch{}, e
	}
	h.batch = b
	return b, nil
}

func (h *nativeLiveSourceAnswerRun) PreviewSourceModel(ctx context.Context, b OwnLiveSourceBatch) (preview LiveEgressPreview, err error) {
	stage := "source-preview-input"
	defer func() { err = nativeLiveStage(stage, err) }()
	if h == nil {
		return LiveEgressPreview{}, modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	stage = "source-preview-current"
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	defer tx.Rollback(ctx)
	stage = "source-preview-exact-batch"
	if r.control.Steps[1].BindingState != modelrequestrun.WaitingSource || !h.batch.valid || !b.valid || b.evidenceDigest != h.batch.evidenceDigest || b.access != h.access {
		return LiveEgressPreview{}, modelegressbudget.ErrDenied
	}
	b.association = h.association()
	stage = "source-preview-payload"
	v, e := h.store.previewLiveSourceEgressTx(ctx, tx, h.access, h.modelInput, b, h.projector)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	stage = "source-preview-commit"
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return LiveEgressPreview{}, e
	}
	return v, nil
}

// Approval is explicit and exact; merely reading Control or possessing a batch
// never approves its model export. The original 062 preview is the sole grant.
func (h *nativeLiveSourceAnswerRun) ApproveSourceModel(ctx context.Context, id, digest string) error {
	if h == nil {
		return modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if r.control.Steps[1].BindingState != modelrequestrun.WaitingSource {
		return modelegressbudget.ErrConflict
	}
	p, e := h.store.approveLiveEgressTx(ctx, tx, h.access, id, digest, h.owner, h.session, h.projector, h.association())
	if e != nil {
		return e
	}
	if p.input != h.modelInput || p.price.Kind != modelegressbudget.LiveToken || !h.batch.valid || p.sourceBatch.evidenceDigest != h.batch.evidenceDigest {
		return modelegressbudget.ErrDenied
	}
	return h.commitCurrent(ctx, tx, r, false)
}

func (h *nativeLiveSourceAnswerRun) BindSourceModel(ctx context.Context, id, digest string, revision int64) (control modelrequestrun.Control, err error) {
	stage := "bind-input"
	defer func() { err = nativeLiveStage(stage, err) }()
	var empty modelrequestrun.Control
	if h == nil || !egressUUID(id) || revision < 1 {
		return empty, modelegressbudget.ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	stage = "bind-current"
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	stage = "bind-revision"
	if r.control.Revision != revision || r.control.State != modelrequestrun.RunRunning || r.control.Steps[0].State != modelrequestrun.StepUnknown || r.control.Steps[1].BindingState != modelrequestrun.WaitingSource || r.control.Steps[1].State != modelrequestrun.StepPlanned || !h.batch.valid {
		return empty, modelegressbudget.ErrConflict
	}
	stage = "bind-read-preview"
	v, e := readLiveEgressPreview(ctx, tx, id)
	if e != nil {
		return empty, e
	}
	stage = "bind-source-replay"
	p, e := h.store.prepareStoredLiveEgressWithSourceRunTx(ctx, tx, h.access, v, h.projector, h.association())
	if e != nil {
		return empty, e
	}
	stage = "bind-exact-source"
	if v.status != "APPROVED" || v.scope != liveModelSourceScope(h.original) || v.owner != h.owner || v.session != h.session || !matchLivePreview(v, p) || p.digest != digest || p.input != h.modelInput || p.sourceBatch.evidenceDigest != h.batch.evidenceDigest {
		return empty, modelegressbudget.ErrDenied
	}
	stage = "bind-update-step"
	tag, e := tx.Exec(ctx, `UPDATE model_request_run_steps SET preview_id=$2,request_digest=$3,source_evidence_digest=$4,binding_state='BOUND',updated_at=clock_timestamp() WHERE run_id=$1 AND ordinal=2 AND binding_state='WAITING_SOURCE' AND preview_id IS NULL AND request_digest IS NULL AND reservation_id IS NULL AND state='PLANNED'`, h.id, id, digest, p.sourceBatch.evidenceDigest)
	if e != nil {
		return empty, egressError(e)
	}
	if tag.RowsAffected() != 1 {
		return empty, modelegressbudget.ErrConflict
	}
	stage = "bind-update-run"
	tag, e = tx.Exec(ctx, `UPDATE model_request_runs SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1 AND revision=$2 AND fence=$3 AND state='RUNNING'`, h.id, revision, h.fence)
	if e != nil {
		return empty, egressError(e)
	}
	if tag.RowsAffected() != 1 {
		return empty, modelegressbudget.ErrConflict
	}
	stage = "bind-read-run"
	r, e = readModelRun(ctx, tx, h.id, h.owner)
	if e != nil {
		return empty, e
	}
	stage = "bind-commit"
	if e = h.commitCurrent(ctx, tx, r, false); e != nil {
		return empty, e
	}
	return modelrequestrun.Clone(r.control), nil
}

type nativeLiveSourceModelDispatch struct {
	run  *nativeLiveSourceAnswerRun
	used atomic.Bool
}

var _ modelgateway.NativeLiveDispatchPort = (*nativeLiveSourceModelDispatch)(nil)

func (p *nativeLiveSourceModelDispatch) CheckCurrent(ctx context.Context, r modelgateway.Request, wire modelgateway.PreparedTencentWire) error {
	if p == nil || p.run == nil {
		return modelegressbudget.ErrDenied
	}
	current, e := p.run.checkStep(ctx, 2)
	if e != nil || !nativeLiveRequestEqual(current.Request(), r) || current.ModelWire().WireDigest() != wire.WireDigest() || !wire.Matches(nativeProviderRequest(r), time.Now()) {
		return modelegressbudget.ErrDenied
	}
	return nil
}
func (p *nativeLiveSourceModelDispatch) ReleaseOnce(ctx context.Context, r modelgateway.Request, wire modelgateway.PreparedTencentWire, invoke func(context.Context) ([]byte, error)) ([]byte, error) {
	if p == nil || p.run == nil || invoke == nil || !p.used.CompareAndSwap(false, true) || p.CheckCurrent(ctx, r, wire) != nil {
		return nil, modelegressbudget.ErrDenied
	}
	h := p.run
	h.mu.Lock()
	prepared, e := h.beginStepLocked(ctx, 2)
	h.mu.Unlock()
	if e != nil || !nativeLiveRequestEqual(prepared.Request(), r) || prepared.ModelWire().WireDigest() != wire.WireDigest() || p.CheckCurrent(ctx, r, wire) != nil {
		return nil, modelegressbudget.ErrDenied
	}
	return invoke(ctx)
}
func (p *nativeLiveSourceModelDispatch) SettleUnknown(ctx context.Context, r modelgateway.Request, wire modelgateway.PreparedTencentWire) error {
	if p == nil || p.run == nil || !p.used.Load() || wire.WireDigest() == "" {
		return modelegressbudget.ErrDenied
	}
	return p.run.settleUnknown(ctx, 2, modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"})
}

// The validated result is produced inside the concrete Gateway call. There is
// no public method accepting a caller-created result or marking UNKNOWN done.
func (h *nativeLiveSourceAnswerRun) CompleteSourceModel(ctx context.Context, adapter *modelgateway.TencentTokenHubAdapter) (completion modelgateway.Result, err error) {
	stage := "model-input"
	defer func() { err = nativeLiveStage(stage, err) }()
	var empty modelgateway.Result
	if h == nil || adapter == nil || !h.modelUsed.CompareAndSwap(false, true) {
		return empty, modelegressbudget.ErrDenied
	}
	stage = "model-reserve"
	h.mu.Lock()
	_, e := h.reserveStepLocked(ctx, 2)
	h.mu.Unlock()
	if e != nil {
		return empty, e
	}
	stage = "model-check"
	p, e := h.checkStep(ctx, 2)
	if e != nil {
		return empty, e
	}
	stage = "model-gateway-construction"
	port := &nativeLiveSourceModelDispatch{run: h}
	gateway, e := modelgateway.NewNativeLiveGateway(h.gate, adapter, port)
	if e != nil {
		return empty, e
	}
	stage = "model-gateway-complete"
	result, e := gateway.Complete(ctx, p.Request())
	if e != nil {
		if failed := h.lastNativeFailure.Load(); failed != nil {
			return empty, errors.Join(e, failed)
		}
		return empty, e
	}
	stage = "model-validated-text"
	if result.Mode != modelgateway.Live || result.Status != modelgateway.Completed || result.Text == "" || result.RunID != p.Request().RunID || result.Agent != p.Request().Agent || result.Candidate != nil || result.Answer != nil || len(result.ToolProposals) != 0 || result.Usage.CostStatus != "UNKNOWN" {
		return empty, modelegressbudget.ErrDenied
	}
	stage = "model-usage-observation"
	if e = h.settleUnknown(ctx, 2, result.Usage); e != nil {
		return empty, e
	}
	stage = "model-final-current"
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, r, e := h.beginCurrent(ctx, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	stage = "model-final-source-replay"
	current, e := h.preparedStepTx(ctx, tx, r, 2)
	if e != nil || !nativeLiveRequestEqual(current.Request(), p.Request()) || r.control.Steps[1].State != modelrequestrun.StepUnknown {
		return empty, modelegressbudget.ErrDenied
	}
	stage = "model-finish-run"
	if _, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='FINISHED',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1 AND fence=$2 AND state='RUNNING'`, h.id, h.fence); e != nil {
		return empty, egressError(e)
	}
	stage = "model-finish-read-run"
	r, e = readModelRun(ctx, tx, h.id, h.owner)
	if e != nil {
		return empty, e
	}
	stage = "model-finish-commit"
	if e = h.commitCurrent(ctx, tx, r, true); e != nil {
		return empty, e
	}
	h.completedResult = cloneValidatedLiveSourceTextResult(result)
	return result, nil
}

// Only the validated native TEXT completion path calls this helper. Keeping
// private copies prevents callers from changing usage through the returned
// result after the original Run has committed its finished fence.
func cloneValidatedLiveSourceTextResult(result modelgateway.Result) *modelgateway.Result {
	saved := result
	saved.Answer = nil
	saved.Candidate = nil
	saved.ToolProposals = nil
	if result.Usage.InputTokens != nil {
		value := *result.Usage.InputTokens
		saved.Usage.InputTokens = &value
	}
	if result.Usage.OutputTokens != nil {
		value := *result.Usage.OutputTokens
		saved.Usage.OutputTokens = &value
	}
	return &saved
}

// Stop retires the original fence without guessing wire or cash status. Unlike
// normal dispatch it does not require current source permission or rollout.
func (h *nativeLiveSourceAnswerRun) Stop(ctx context.Context) error {
	if h == nil || h.store == nil {
		return modelegressbudget.ErrDenied
	}
	tx, e := h.store.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := h.store.egressOwner(ctx, tx, h.access)
	if e != nil {
		return e
	}
	if owner != h.owner || session != h.session {
		return modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil {
		return e
	}
	if r.kind != modelrequestrun.LiveSourceAnswer || r.control.Fence != h.fence {
		return modelegressbudget.ErrDenied
	}
	if r.control.State == modelrequestrun.RunPlanned || r.control.State == modelrequestrun.RunRunning {
		if _, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='STOPPED',revision=revision+1,fence=fence+1,updated_at=clock_timestamp() WHERE id=$1`, h.id); e != nil {
			return egressError(e)
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

// Owner-readable metadata remains the original v1 history path. Its v2 shape
// is data only and exposes no query, source passages, response or gate ticket.
func (h *nativeLiveSourceAnswerRun) Read(ctx context.Context) (modelrequestrun.Control, error) {
	if h == nil || h.store == nil {
		return modelrequestrun.Control{}, modelegressbudget.ErrDenied
	}
	return h.store.ReadOwnLocalModelRun(ctx, h.access, h.id)
}

// Keep accidental formatting of sealed private associations redacted as well.
func (*nativeLiveSourceRunAssociation) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "nativeLiveSourceRunAssociation{redacted}")
}
