package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
	"github.com/jackc/pgx/v5"
)

var _ modelegressbudget.LocalModelRunPort = (*Store)(nil)

type storedModelRun struct {
	control                                       modelrequestrun.Control
	session, agent, generation, source, authority string
	kind                                          string
}
type modelRunAccounting struct {
	run     storedModelRun
	ordinal int
}

// The original budget ledger is the only accounting authority. Association is
// global by original operation UUID, never merely scoped to a caller's owner.
func modelRunTables(ctx context.Context, tx pgx.Tx) (bool, error) {
	var ok bool
	e := tx.QueryRow(ctx, `SELECT to_regclass('public.model_request_run_steps') IS NOT NULL`).Scan(&ok)
	return ok, egressError(e)
}
func modelOperationLock(ctx context.Context, tx pgx.Tx, op string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.model-operation:'||$1,0))`, op)
	return egressError(e)
}
func denyLinkedModelOperation(ctx context.Context, tx pgx.Tx, op string) error {
	if e := modelOperationLock(ctx, tx, op); e != nil {
		return e
	}
	exists, e := modelRunTables(ctx, tx)
	if e != nil || !exists {
		return e
	}
	var linked bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_request_run_steps WHERE operation_id=$1)`, op).Scan(&linked)
	if e != nil {
		return egressError(e)
	}
	if linked {
		return modelegressbudget.ErrDenied
	}
	return nil
}
func readModelRun(ctx context.Context, tx pgx.Tx, id, owner string) (storedModelRun, error) {
	var r storedModelRun
	c := &r.control
	c.SchemaVersion = modelrequestrun.SchemaVersion
	c.Owner = actorref.PrincipalRef{Type: actorref.Person, ID: owner}
	e := tx.QueryRow(ctx, `SELECT id::text,task_id::text,root_trace_id::text,binding_id::text,state,revision,fence,created_at,updated_at,deadline_at,lease_until,session_id::text,agent_id::text,task_generation,source_token,authority_token,COALESCE(to_jsonb(model_request_runs)->>'run_kind','LOCAL_RETRY') FROM model_request_runs WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, owner).Scan(&c.ModelRunID, &c.TaskID, &c.RootTraceID, &c.BindingID, &c.State, &c.Revision, &c.Fence, &c.CreatedAt, &c.UpdatedAt, &c.DeadlineAt, &c.LeaseUntil, &r.session, &r.agent, &r.generation, &r.source, &r.authority, &r.kind)
	if e != nil {
		return r, egressError(e)
	}
	if r.kind == modelrequestrun.LiveSourceAnswer {
		c.SchemaVersion = modelrequestrun.LiveSourceSchemaVersion
		c.Kind = r.kind
	} else if r.kind != "LOCAL_RETRY" {
		return r, modelegressbudget.ErrDenied
	}
	rows, e := tx.Query(ctx, `SELECT ordinal,operation_id::text,COALESCE(preview_id::text,''),price_version,COALESCE(request_digest,''),state,COALESCE(reservation_id::text,''),COALESCE(to_jsonb(model_request_run_steps)->>'step_kind','LOCAL_MODEL'),COALESCE(to_jsonb(model_request_run_steps)->>'binding_state','BOUND'),COALESCE(to_jsonb(model_request_run_steps)->>'source_evidence_digest',''),COALESCE((to_jsonb(model_request_run_steps)->>'planned_max_output_tokens')::integer,0) FROM model_request_run_steps WHERE run_id=$1 ORDER BY ordinal FOR UPDATE`, id)
	if e != nil {
		return r, egressError(e)
	}
	defer rows.Close()
	for rows.Next() {
		var step modelrequestrun.Step
		if e = rows.Scan(&step.Ordinal, &step.OperationID, &step.PreviewID, &step.PriceVersion, &step.RequestDigest, &step.State, &step.ReservationID, &step.Kind, &step.BindingState, &step.SourceEvidenceDigest, &step.MaxOutputTokens); e != nil {
			return r, egressError(e)
		}
		if r.kind == "LOCAL_RETRY" {
			step.Kind = ""
			step.BindingState = ""
			step.SourceEvidenceDigest = ""
			step.MaxOutputTokens = 0
		}
		c.Steps = append(c.Steps, step)
	}
	return r, egressError(rows.Err())
}
func lockModelRunAccounting(ctx context.Context, tx pgx.Tx, owner, op string) (*modelRunAccounting, error) {
	exists, e := modelRunTables(ctx, tx)
	if e != nil || !exists {
		return nil, e
	}
	var id string
	var ordinal int
	e = tx.QueryRow(ctx, `SELECT run_id::text,ordinal FROM model_request_run_steps WHERE operation_id=$1`, op).Scan(&id, &ordinal)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, egressError(e)
	}
	r, e := readModelRun(ctx, tx, id, owner)
	if e != nil {
		return nil, e
	}
	return &modelRunAccounting{r, ordinal}, nil
}
func syncModelRunAccounting(ctx context.Context, tx pgx.Tx, linked *modelRunAccounting, r modelegressbudget.Reservation) error {
	if linked == nil {
		return nil
	}
	steps := linked.run.control.Steps
	if linked.ordinal < 1 || linked.ordinal > len(steps) {
		return modelegressbudget.ErrDenied
	}
	step := steps[linked.ordinal-1]
	if step.OperationID != r.OperationID || step.ReservationID != r.OperationID || step.PreviewID != r.PreviewID || step.PriceVersion != r.PriceVersion || step.RequestDigest != r.RequestDigest {
		return modelegressbudget.ErrDenied
	}
	if string(step.State) == r.State {
		return nil
	}
	switch r.State {
	case "SETTLED", "UNKNOWN", "CANCELLED_BEFORE_SEND":
	default:
		return modelegressbudget.ErrDenied
	}
	_, e := tx.Exec(ctx, `UPDATE model_request_run_steps SET state=$2,updated_at=clock_timestamp() WHERE operation_id=$1`, r.OperationID, r.State)
	if e != nil {
		return egressError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE model_request_runs SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, linked.run.control.ModelRunID)
	return egressError(e)
}

// A handle is minted only after original approved routes and native metadata
// commit together. It is tied to this store/session/source generation and one
// original rollout ticket. Readable JSON metadata can never reconstruct it.
type nativeModelRunHandle struct {
	mu               sync.Mutex
	store            *Store
	access           agentevent.Access
	controller       *agentfeature.Controller
	ticket           agentfeature.Ticket
	id               string
	fence            int64
	proof            *nativeLocalRetryProof
	failureOrdinal   int
	outputScope      *nativeModelOutputScope
	outputRepairUsed bool
	planner          *nativePlannerGoal
	plannerView      *agentplanner.View
	toolPlan         *nativeToolPlan
	activeToolPolicy *nativeToolPolicy
}
type nativeModelRunProof struct{ handle *nativeModelRunHandle }

func (p *nativeModelRunProof) Remaining(now time.Time) time.Duration {
	if p == nil || p.handle == nil {
		return 0
	}
	p.handle.mu.Lock()
	defer p.handle.mu.Unlock()
	return p.handle.proof.checkpoint.Remaining(now)
}
func (*nativeModelRunProof) MarshalJSON() ([]byte, error) {
	return nil, modelegressbudget.ErrServerOnly
}
func (p *nativeModelRunProof) UnmarshalJSON([]byte) error {
	p.handle = nil
	return modelegressbudget.ErrServerOnly
}
func (*nativeModelRunHandle) MarshalJSON() ([]byte, error) {
	return nil, modelegressbudget.ErrServerOnly
}
func (h *nativeModelRunHandle) UnmarshalJSON([]byte) error { return modelegressbudget.ErrServerOnly }

func (s *Store) CreateOwnLocalModelRun(ctx context.Context, a agentevent.Access, id string, bindings []modelegressbudget.LocalRetryBinding, c *agentfeature.Controller, ticket agentfeature.Ticket) (modelegressbudget.LocalModelRunHandle, modelrequestrun.Control, error) {
	return s.createOwnLocalModelRun(ctx, a, id, bindings, c, ticket, nil)
}
func (s *Store) createOwnLocalModelRun(ctx context.Context, a agentevent.Access, id string, bindings []modelegressbudget.LocalRetryBinding, c *agentfeature.Controller, ticket agentfeature.Ticket, goal *nativePlannerGoal) (modelegressbudget.LocalModelRunHandle, modelrequestrun.Control, error) {
	var empty modelrequestrun.Control
	if !egressUUID(id) || len(bindings) < 1 || len(bindings) > modelresilience.MaxAttempts || c == nil || !c.Current(ticket) {
		return nil, empty, modelegressbudget.ErrInvalid
	}
	bindings = append([]modelegressbudget.LocalRetryBinding(nil), bindings...)
	seen := map[string]bool{}
	ops := make([]string, 0, len(bindings))
	for _, b := range bindings {
		in := b.Input
		if !egressUUID(in.OperationID) || !egressUUID(in.PreviewID) || !egressUUID(in.RootTraceID) || !egressUUID(in.TaskID) || seen[in.OperationID] || in.RootTraceID != bindings[0].Input.RootTraceID || in.TaskID != bindings[0].Input.TaskID {
			return nil, empty, modelegressbudget.ErrInvalid
		}
		seen[in.OperationID] = true
		ops = append(ops, in.OperationID)
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
	sort.Strings(ops)
	for _, op := range ops {
		if e = modelOperationLock(ctx, tx, op); e != nil {
			return nil, empty, e
		}
		var present bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_budget_reservations WHERE operation_id=$1) OR EXISTS(SELECT 1 FROM model_request_run_steps WHERE operation_id=$1)`, op).Scan(&present)
		if e != nil {
			return nil, empty, egressError(e)
		}
		if present {
			return nil, empty, modelegressbudget.ErrConflict
		}
	}
	proof := &nativeLocalRetryProof{store: s, access: a, owner: owner, session: session}
	var expiry time.Time
	for _, b := range bindings {
		route, bound, e := s.readNativeRetryRoute(ctx, tx, a, owner, session, b)
		if e != nil {
			return nil, empty, e
		}
		if len(proof.routes) > 0 {
			first := proof.routes[0]
			p := route.preview
			if p.BindingID != first.preview.BindingID || p.SourceToken != first.preview.SourceToken || p.AuthorityToken != first.preview.AuthorityToken || p.Request.Agent != first.preview.Request.Agent || route.taskGeneration != first.taskGeneration {
				return nil, empty, modelegressbudget.ErrDenied
			}
		}
		proof.routes = append(proof.routes, route)
		expiry = retryMinimum(expiry, bound)
	}
	first := proof.routes[0]
	if goal != nil {
		if len(bindings) > agentplanner.MaxModelCalls || goal.store != s || goal.access != a || !goal.ready || goal.Remaining(time.Now()) <= 0 ||
			goal.task != first.binding.Input.TaskID || goal.session != session || goal.agent != first.preview.Request.Agent.AgentID || goal.source != first.preview.SourceToken || goal.generation != first.taskGeneration ||
			first.preview.Request.OutputMode != modelgateway.Structured || first.preview.Request.OutputSchemaVersion != "air.answer.v1" || len(first.preview.Request.ToolAllowlist) != 0 {
			return nil, empty, agentplanner.ErrDenied
		}
		expiry = retryMinimum(expiry, goal.view.ValidUntil)
	}
	if id == first.preview.BindingID {
		return nil, empty, modelegressbudget.ErrInvalid
	}
	// Initial native Session expiry is part of the immutable original boundary.
	cp, e := captureLocalAttemptCheckpoint(ctx, tx, session, first.binding.Input.OperationID, first.preview.Request, expiry, expiry, c, ticket)
	if e != nil {
		return nil, empty, e
	}
	expiry = retryMinimum(expiry, cp.ValidUntil())
	if d, ok := ctx.Deadline(); ok {
		expiry = retryMinimum(expiry, d)
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_request_runs(id,owner_id,session_id,agent_id,root_trace_id,task_id,binding_id,task_generation,source_token,authority_token,state,deadline_at,lease_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'PLANNED',$11,$11)`, id, owner, session, first.preview.Request.Agent.AgentID, first.binding.Input.RootTraceID, first.binding.Input.TaskID, first.preview.BindingID, first.taskGeneration, first.preview.SourceToken, first.preview.AuthorityToken, expiry)
	if e != nil {
		return nil, empty, egressError(e)
	}
	for i, route := range proof.routes {
		_, e = tx.Exec(ctx, `INSERT INTO model_request_run_steps(run_id,ordinal,operation_id,preview_id,price_version,request_digest) VALUES($1,$2,$3,$4,$5,$6)`, id, i+1, route.binding.Input.OperationID, route.binding.Input.PreviewID, route.preview.PriceVersion, route.preview.RequestDigest)
		if e != nil {
			return nil, empty, egressError(e)
		}
	}
	r, e := readModelRun(ctx, tx, id, owner)
	if e != nil {
		return nil, empty, e
	}
	h := &nativeModelRunHandle{store: s, access: a, controller: c, ticket: ticket, id: id, fence: r.control.Fence, proof: proof, planner: goal}
	if e = h.captureOutputScope(ctx, tx); e != nil {
		return nil, empty, e
	}
	cp, e = h.finalCheck(ctx, tx, r, first.binding.Input.OperationID, false)
	if e != nil {
		return nil, empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, empty, egressError(e)
	}
	proof.checkpoint = cp
	if !cp.ValidAt(time.Now()) || !c.Current(ticket) {
		return nil, empty, modelegressbudget.ErrDenied
	}
	return h, modelrequestrun.Clone(r.control), nil
}

// All caller-visible operations begin Session/account -> owner advisory -> Run
// -> ordered steps; original budget/source helpers never commit inside this tx.
func (h *nativeModelRunHandle) begin(ctx context.Context, a agentevent.Access, c *agentfeature.Controller, allowFinished bool) (pgx.Tx, storedModelRun, error) {
	var empty storedModelRun
	if h == nil || h.store == nil || a != h.access || c != h.controller || c == nil || !c.Current(h.ticket) || !h.proof.checkpoint.ValidAt(time.Now()) {
		return nil, empty, modelegressbudget.ErrDenied
	}
	tx, e := h.store.beginEgress(ctx)
	if e != nil {
		return nil, empty, e
	}
	fail := func(e error) (pgx.Tx, storedModelRun, error) { tx.Rollback(ctx); return nil, empty, e }
	owner, session, e := h.store.egressOwner(ctx, tx, a)
	if e != nil {
		return fail(e)
	}
	if owner != h.proof.owner || session != h.proof.session {
		return fail(modelegressbudget.ErrDenied)
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return fail(e)
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil {
		return fail(e)
	}
	if r.kind != "LOCAL_RETRY" || r.session != session || r.agent != h.proof.routes[0].preview.Request.Agent.AgentID || r.generation != h.proof.routes[0].taskGeneration || r.source != h.proof.routes[0].preview.SourceToken || r.authority != h.proof.routes[0].preview.AuthorityToken || r.control.Fence != h.fence {
		return fail(modelegressbudget.ErrDenied)
	}
	if r.control.State != modelrequestrun.RunPlanned && r.control.State != modelrequestrun.RunRunning && !(allowFinished && r.control.State == modelrequestrun.RunFinished) {
		return fail(modelegressbudget.ErrDenied)
	}
	if _, e = h.finalCheck(ctx, tx, r, h.proof.routes[0].binding.Input.OperationID, allowFinished); e != nil {
		return fail(e)
	}
	return tx, r, nil
}
func (h *nativeModelRunHandle) route(op string) (nativeLocalRetryRoute, int, bool) {
	for i, r := range h.proof.routes {
		if r.binding.Input.OperationID == op {
			return r, i + 1, true
		}
	}
	return nativeLocalRetryRoute{}, 0, false
}
func (h *nativeModelRunHandle) finalCheck(ctx context.Context, tx pgx.Tx, r storedModelRun, op string, allowFinished bool) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	route, _, ok := h.route(op)
	if !ok {
		return empty, modelegressbudget.ErrDenied
	}
	expiry := retryMinimum(retryMinimum(r.control.DeadlineAt, r.control.LeaseUntil), h.proof.checkpoint.ValidUntil())
	// The initial capture has no checkpoint until the first committed receipt.
	if h.proof.checkpoint.ValidUntil().IsZero() {
		expiry = retryMinimum(r.control.DeadlineAt, r.control.LeaseUntil)
	}
	for _, old := range h.proof.routes {
		current, bound, e := h.store.readNativeRetryRoute(ctx, tx, h.access, h.proof.owner, h.proof.session, old.binding)
		if e != nil {
			return empty, e
		}
		if !retryRouteUnchanged(old, current) {
			return empty, modelegressbudget.ErrDenied
		}
		expiry = retryMinimum(expiry, bound)
	}
	var live bool
	var deadline time.Time
	e := tx.QueryRow(ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n) SELECT fence=$2 AND session_id=$3 AND deadline_at>n AND lease_until>n AND (state IN('PLANNED','RUNNING') OR ($4 AND state='FINISHED')),LEAST(deadline_at,lease_until) FROM model_request_runs CROSS JOIN c WHERE id=$1`, h.id, h.fence, h.proof.session, allowFinished).Scan(&live, &deadline)
	if e != nil {
		return empty, egressError(e)
	}
	if !live {
		return empty, modelegressbudget.ErrDenied
	}
	expiry = retryMinimum(expiry, deadline)
	if h.planner != nil {
		if h.planner.Remaining(time.Now()) <= 0 {
			return empty, agentplanner.ErrChanged
		}
		expiry = retryMinimum(expiry, h.planner.view.ValidUntil)
	}
	if bound, e := h.revalidateOutputScope(ctx, tx, allowFinished); e != nil {
		return empty, e
	} else if !bound.IsZero() {
		expiry = retryMinimum(expiry, bound)
	}
	return captureLocalAttemptCheckpoint(ctx, tx, h.proof.session, op, route.preview.Request, expiry, expiry, h.controller, h.ticket)
}
func (h *nativeModelRunHandle) commit(ctx context.Context, tx pgx.Tx, r storedModelRun, op string, finished bool) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := h.finalCheck(ctx, tx, r, op, finished)
	if e != nil {
		return cp, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.LocalReleaseCheckpoint{}, egressError(e)
	}
	// Only shorten the process-local bound; normal idle refresh cannot revive it.
	if h.proof.checkpoint.ValidUntil().IsZero() || cp.Remaining(time.Now()) < h.proof.checkpoint.Remaining(time.Now()) {
		h.proof.checkpoint = cp
	}
	if !cp.ValidAt(time.Now()) || !h.controller.Current(h.ticket) {
		return modelegressbudget.LocalReleaseCheckpoint{}, modelegressbudget.ErrDenied
	}
	return cp, nil
}
func (h *nativeModelRunHandle) ReserveOwnModelAttempt(ctx context.Context, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller) (modelegressbudget.Reservation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelegressbudget.Reservation
	route, ordinal, ok := h.route(in.OperationID)
	if !ok || in != route.binding.Input {
		return empty, modelegressbudget.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if ordinal > len(r.control.Steps) || r.control.Steps[ordinal-1].State != modelrequestrun.StepPlanned {
		return empty, modelegressbudget.ErrConflict
	}
	if ordinal > 1 && h.failureOrdinal != ordinal-1 {
		return empty, modelegressbudget.ErrDenied
	}
	for i, step := range r.control.Steps {
		if i < ordinal-1 && (step.State != modelrequestrun.StepUnknown && step.State != modelrequestrun.StepSettled) {
			return empty, modelegressbudget.ErrDenied
		}
		if step.State == modelrequestrun.StepReserved || step.State == modelrequestrun.StepInFlight {
			return empty, modelegressbudget.ErrConflict
		}
	}
	out, e := h.store.reserveModelAttemptTx(ctx, tx, a, in, c, h.ticket, h.proof.owner, h.proof.session)
	if e != nil {
		return empty, e
	}
	if out.State != "RESERVED" {
		return empty, modelegressbudget.ErrDenied
	}
	_, e = tx.Exec(ctx, `UPDATE model_request_run_steps SET state='RESERVED',reservation_id=operation_id,updated_at=clock_timestamp() WHERE run_id=$1 AND ordinal=$2`, h.id, ordinal)
	if e != nil {
		return empty, egressError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='RUNNING',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
	if e != nil {
		return empty, egressError(e)
	}
	_, e = h.commit(ctx, tx, r, in.OperationID, false)
	if e != nil {
		return empty, e
	}
	return out, nil
}
func (h *nativeModelRunHandle) BeginOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, op string, c *agentfeature.Controller) (modelgateway.Request, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelgateway.Request
	_, ordinal, ok := h.route(op)
	if !ok {
		return empty, modelegressbudget.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if r.control.Steps[ordinal-1].State != modelrequestrun.StepReserved {
		return empty, modelegressbudget.ErrDenied
	}
	out, e := h.store.beginModelAttemptTx(ctx, tx, a, op, c, h.ticket, h.proof.owner, h.proof.session)
	if e != nil {
		return empty, e
	}
	_, e = tx.Exec(ctx, `UPDATE model_request_run_steps SET state='IN_FLIGHT',updated_at=clock_timestamp() WHERE operation_id=$1`, op)
	if e != nil {
		return empty, egressError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE model_request_runs SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
	if e != nil {
		return empty, egressError(e)
	}
	_, e = h.commit(ctx, tx, r, op, false)
	if e != nil {
		return empty, e
	}
	return out, nil
}
func (h *nativeModelRunHandle) SettleOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, op string, u modelegressbudget.LocalUsage) (modelegressbudget.Reservation, error) {
	if _, _, ok := h.route(op); !ok || a != h.access {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	return h.store.SettleOwnLocalModelAttempt(ctx, a, op, u)
}
func (h *nativeModelRunHandle) CancelOwnReservedModelAttempt(ctx context.Context, a agentevent.Access, op string) (modelegressbudget.Reservation, error) {
	if _, _, ok := h.route(op); !ok || a != h.access {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	return h.store.CancelOwnReservedModelAttempt(ctx, a, op)
}
func (h *nativeModelRunHandle) ReadOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, op string) (modelegressbudget.AttemptControl, error) {
	if _, _, ok := h.route(op); !ok || a != h.access {
		return modelegressbudget.AttemptControl{}, modelegressbudget.ErrDenied
	}
	return h.store.ReadOwnLocalModelAttempt(ctx, a, op)
}

// Stopping retires only this run's dispatch fence. Unknown/in-flight accounting
// stays untouched and can still be reconciled by its original operation ID.
func (h *nativeModelRunHandle) StopOwnLocalModelRun(ctx context.Context, a agentevent.Access) error {
	if h == nil || h.store == nil || a != h.access {
		return modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, e := h.store.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := h.store.egressOwner(ctx, tx, a)
	if e != nil {
		return e
	}
	if owner != h.proof.owner {
		return modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil {
		return e
	}
	if r.kind != "LOCAL_RETRY" || r.control.Fence != h.fence {
		return modelegressbudget.ErrDenied
	}
	if r.control.State == modelrequestrun.RunPlanned || r.control.State == modelrequestrun.RunRunning {
		_, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='STOPPED',revision=revision+1,fence=fence+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
		if e != nil {
			return egressError(e)
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

func (h *nativeModelRunHandle) CaptureOwnLocalRetryPlan(ctx context.Context, a agentevent.Access, bindings []modelegressbudget.LocalRetryBinding, c *agentfeature.Controller) (modelegressbudget.LocalRetryProof, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(bindings) != len(h.proof.routes) {
		return nil, modelegressbudget.ErrDenied
	}
	for i, b := range bindings {
		if b != h.proof.routes[i].binding {
			return nil, modelegressbudget.ErrDenied
		}
	}
	tx, r, e := h.begin(ctx, a, c, false)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = h.commit(ctx, tx, r, bindings[0].Input.OperationID, false); e != nil {
		return nil, e
	}
	return &nativeModelRunProof{h}, nil
}
func (h *nativeModelRunHandle) proofOK(p modelegressbudget.LocalRetryProof) bool {
	v, ok := p.(*nativeModelRunProof)
	return ok && v != nil && v.handle == h
}
func (h *nativeModelRunHandle) RevalidateOwnLocalRetryPlan(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.proofOK(p) {
		return modelegressbudget.LocalReleaseCheckpoint{}, modelegressbudget.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, true)
	if e != nil {
		return modelegressbudget.LocalReleaseCheckpoint{}, e
	}
	defer tx.Rollback(ctx)
	return h.commit(ctx, tx, r, h.proof.routes[0].binding.Input.OperationID, true)
}
func (h *nativeModelRunHandle) CheckOwnLocalRetryFailure(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, f modelegressbudget.LocalRetryFailure, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelegressbudget.LocalReleaseCheckpoint
	op, req, ok := f.OperationRequest()
	if !ok || !h.proofOK(p) {
		return empty, modelegressbudget.ErrDenied
	}
	route, ordinal, ok := h.route(op)
	if !ok || !modelRunRequestEqual(req, route.preview.Request) {
		return empty, modelegressbudget.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	reservation, e := readEgressReservation(ctx, tx, op, h.proof.owner)
	if e != nil {
		return empty, e
	}
	if (reservation.State != "UNKNOWN" && reservation.State != "SETTLED") || reservation.RequestDigest != route.preview.RequestDigest || reservation.PriceVersion != route.preview.PriceVersion || reservation.PreviewID != route.binding.Input.PreviewID || string(r.control.Steps[ordinal-1].State) != reservation.State {
		return empty, modelegressbudget.ErrDenied
	}
	cp, e := h.commit(ctx, tx, r, op, false)
	if e == nil {
		h.failureOrdinal = ordinal
	}
	return cp, e
}
func modelRunRequestEqual(a, b modelgateway.Request) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}
func (h *nativeModelRunHandle) CheckOwnLocalModelAttemptDispatch(ctx context.Context, a agentevent.Access, op string, c *agentfeature.Controller, req modelgateway.Request, dest modelegressbudget.LocalAttemptDestination) (modelegressbudget.LocalReleaseCheckpoint, error) {
	return h.dispatch(ctx, a, nil, op, c, req, dest)
}
func (h *nativeModelRunHandle) CheckOwnLocalRetryDispatch(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, op string, req modelgateway.Request, dest modelegressbudget.LocalAttemptDestination, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	if !h.proofOK(p) {
		return modelegressbudget.LocalReleaseCheckpoint{}, modelegressbudget.ErrDenied
	}
	return h.dispatch(ctx, a, p, op, c, req, dest)
}
func (h *nativeModelRunHandle) dispatch(ctx context.Context, a agentevent.Access, _ modelegressbudget.LocalRetryProof, op string, c *agentfeature.Controller, req modelgateway.Request, dest modelegressbudget.LocalAttemptDestination) (modelegressbudget.LocalReleaseCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelegressbudget.LocalReleaseCheckpoint
	route, ordinal, ok := h.route(op)
	if !ok || dest != route.binding.Destination || !modelRunRequestEqual(req, route.preview.Request) {
		return empty, modelegressbudget.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if r.control.Steps[ordinal-1].State != modelrequestrun.StepInFlight {
		return empty, modelegressbudget.ErrDenied
	}
	if _, e = h.store.checkLocalModelDispatchTx(ctx, tx, a, op, c, h.ticket, req, dest, h.proof.owner, h.proof.session); e != nil {
		return empty, e
	}
	return h.commit(ctx, tx, r, op, false)
}
func (h *nativeModelRunHandle) ReleaseOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, op string, c *agentfeature.Controller, req modelgateway.Request, b modelegressbudget.LocalResultBuffer) (modelegressbudget.LocalReleaseCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelegressbudget.LocalReleaseCheckpoint
	result, e := validateLocalModelResult(op, req, b)
	if e != nil {
		return empty, e
	}
	if e = h.validateOutputEntities(result); e != nil {
		return empty, e
	}
	var plan *agentplanner.View
	if h.planner != nil {
		v, err := h.buildReadonlyPlan(result)
		if err != nil {
			return empty, err
		}
		plan = &v
	}
	route, _, ok := h.route(op)
	if !ok || !modelRunRequestEqual(req, route.preview.Request) {
		return empty, modelegressbudget.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if _, e = h.store.releaseLocalModelResultTx(ctx, tx, a, op, c, h.ticket, req, result, h.proof.owner, h.proof.session); e != nil {
		return empty, e
	}
	// Normative ephemeral result plus native accounting/source proof, not merely
	// UNKNOWN accounting, is the only path marking a run FINISHED.
	_, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='FINISHED',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
	if e != nil {
		return empty, egressError(e)
	}
	cp, e := h.commit(ctx, tx, r, op, true)
	if e == nil && plan != nil {
		plan.ValidUntil = retryMinimum(plan.ValidUntil, cp.ValidUntil())
		h.plannerView = plan
	}
	return cp, e
}

func (s *Store) ReadOwnLocalModelRun(ctx context.Context, a agentevent.Access, id string) (modelrequestrun.Control, error) {
	var empty modelrequestrun.Control
	if !egressUUID(id) {
		return empty, modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return empty, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return empty, e
	}
	r, e := readModelRun(ctx, tx, id, owner)
	if e != nil {
		return empty, e
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, egressError(e)
	}
	return modelrequestrun.Clone(r.control), nil
}
func (s *Store) CancelOwnLocalModelRun(ctx context.Context, a agentevent.Access, id string, revision int64) (modelrequestrun.Control, error) {
	var empty modelrequestrun.Control
	if !egressUUID(id) || revision < 1 {
		return empty, modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return empty, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return empty, e
	}
	r, e := readModelRun(ctx, tx, id, owner)
	if e != nil {
		return empty, e
	}
	if r.kind != "LOCAL_RETRY" {
		return empty, modelegressbudget.ErrDenied
	}
	if r.control.Revision != revision {
		return empty, modelegressbudget.ErrConflict
	}
	if r.control.State == modelrequestrun.RunPlanned || r.control.State == modelrequestrun.RunRunning {
		for i, step := range r.control.Steps {
			if step.State == modelrequestrun.StepReserved {
				out, e := s.finishModelAttemptTx(ctx, tx, step.OperationID, modelegressbudget.LocalUsage{}, true, owner, session)
				if e != nil {
					return empty, e
				}
				if e = syncModelRunAccounting(ctx, tx, &modelRunAccounting{r, i + 1}, out); e != nil {
					return empty, e
				}
			}
		}
		_, e = tx.Exec(ctx, `UPDATE model_request_runs SET state='CANCELLED',revision=revision+1,fence=fence+1,updated_at=clock_timestamp() WHERE id=$1`, id)
		if e != nil {
			return empty, egressError(e)
		}
	}
	r, e = readModelRun(ctx, tx, id, owner)
	if e != nil {
		return empty, e
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, egressError(e)
	}
	return modelrequestrun.Clone(r.control), nil
}
