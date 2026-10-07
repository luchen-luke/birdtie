package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

// These are private validation facts. They neither enter the provider request
// nor grant MODEL_EGRESS/TASK_CONTEXT_READ/Memory or a domain action.
type modelOutputFence struct {
	id, owner, session string
	fence              int64
	finished           bool
	toolPolicy         *nativeToolPolicy
}
type nativeModelOutputScope struct {
	access  arp.Access
	query   arp.Query
	initial arp.Receipt
	refs    []modelgateway.EntityProposal
}

// The exact original source payload is also the ModelRun linearization point.
// It observes the dispatch fence and clock in the same statement, after every
// preceding source/account/Run/metadata lock wait in the existing transaction.
func modelOutputProjectionStatement(f *modelOutputFence, args ...any) (string, []any) {
	if f == nil {
		return resultProjectionSQL(), args
	}
	sql := `SELECT p.* FROM (` + resultProjectionSQL() + `) p(observed_at,valid_until,items,commercial_refs,own_raw,proof,authority,activity_rows,place_rows,current_session,action_raw)
 WHERE EXISTS(SELECT 1 FROM model_request_runs mr WHERE mr.id=$7 AND mr.fence=$8 AND mr.owner_id=$2 AND mr.session_id=$9
 AND mr.deadline_at>p.observed_at AND mr.lease_until>p.observed_at
 AND (mr.state IN('PLANNED','RUNNING') OR ($10 AND mr.state='FINISHED')))`
	args = append(args, f.id, f.fence, f.session, f.finished)
	if f.toolPolicy != nil {
		return nativeToolProjectionStatement(sql, args, f.toolPolicy)
	}
	return sql, args
}

func (h *nativeModelRunHandle) captureOutputScope(ctx context.Context, tx pgx.Tx) error {
	if h.proof.routes[0].preview.Request.OutputMode != modelgateway.Structured {
		return nil // Original scalar output does not acquire a structured scope.
	}
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND principal_type='person' FOR SHARE`, h.proof.routes[0].binding.Input.TaskID, h.proof.owner))
	if e != nil {
		return modelgateway.ErrOutputSource
	}
	switch task.Intent {
	case agentworkspace.FindActivity, agentworkspace.AreaDiscovery, agentworkspace.RefineResults, agentworkspace.CompareResults:
	default:
		if h.planner != nil {
			return modelgateway.ErrOutputSource
		}
		return nil // Legacy scalar/query-only tasks acquire no entity authority.
	}
	q := arp.Query{CityID: task.CityID, Kind: "activity", SearchTerm: task.Filters["searchTerm"], Category: task.Filters["category"], TimePreference: task.Filters["timePreference"], Closer: task.Filters["distancePreference"] == "closer", Comparison: task.Intent == agentworkspace.CompareResults, CompareIDs: []string{}}
	if q.Comparison {
		q.CompareIDs = strings.Split(task.Filters["resultIDs"], ",")
	}
	b, e := agentworkspace.BoundsFromFilters(task.Filters)
	if e != nil {
		return modelgateway.ErrOutputSource
	}
	if b != nil {
		q.Bounds = &arp.Bounds{West: b.West, South: b.South, East: b.East, North: b.North}
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil || !q.Valid() {
		return modelgateway.ErrOutputSource
	}
	scope := &nativeModelOutputScope{access: arp.Access{Actor: identity.Actor{ID: h.proof.owner, AccountType: "person"}, SessionDigest: h.access.SessionDigest, TaskID: task.ID, ExpectedTask: raw}, query: q}
	fence := &modelOutputFence{id: h.id, owner: h.proof.owner, session: h.proof.session, fence: h.fence}
	scope.initial, e = h.store.captureAgentResultProjectionTx(ctx, tx, scope.access, q, fence)
	if e != nil {
		return modelgateway.ErrOutputSource
	}
	for _, ref := range scope.initial.PublicCommercialRefs {
		if ref.Type == "activity" {
			scope.refs = append(scope.refs, modelgateway.EntityProposal{Type: "ACTIVITY", ID: ref.ID})
		}
	}
	h.outputScope = scope
	return nil
}

func unchangedOutputProjection(a, b arp.Receipt) bool {
	left, right := append([]arp.Item(nil), a.Items...), append([]arp.Item(nil), b.Items...)
	for i := range left {
		left[i].ActionsValidUntil = nil
	}
	for i := range right {
		right[i].ActionsValidUntil = nil
	}
	return a.ValidUntil.After(b.ObservedAt) && a.Proof == b.Proof && reflect.DeepEqual(left, right) && reflect.DeepEqual(a.PublicCommercialRefs, b.PublicCommercialRefs) && reflect.DeepEqual(a.Activities, b.Activities) && reflect.DeepEqual(a.Places, b.Places)
}
func (h *nativeModelRunHandle) revalidateOutputScope(ctx context.Context, tx pgx.Tx, finished bool) (time.Time, error) {
	if h.outputScope == nil {
		return time.Time{}, nil
	}
	s := h.outputScope
	f := &modelOutputFence{id: h.id, owner: h.proof.owner, session: h.proof.session, fence: h.fence, finished: finished}
	f.toolPolicy = h.activeToolPolicy
	r, e := h.store.captureAgentResultProjectionTx(ctx, tx, s.access, s.query, f)
	if e != nil || !unchangedOutputProjection(s.initial, r) {
		return time.Time{}, modelgateway.ErrOutputSource
	}
	return retryMinimum(s.initial.ValidUntil, r.ValidUntil), nil
}
func (h *nativeModelRunHandle) validateOutputEntities(r modelgateway.Result) error {
	var refs []modelgateway.EntityProposal
	if h.outputScope != nil {
		refs = h.outputScope.refs
	}
	return modelgateway.ValidateEntityScope(r, refs)
}

var _ modelegressbudget.LocalOutputPort = (*nativeModelRunHandle)(nil)

// A format repair consumes the next original 088 step and original 062 budget.
// This method never creates a preview, alters its bytes or refunds unknown use.
func (h *nativeModelRunHandle) CheckOwnLocalOutputFailure(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, f modelegressbudget.LocalOutputFailure, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelegressbudget.LocalReleaseCheckpoint
	op, req, ok := f.OperationRequest()
	if !ok || !h.proofOK(p) || h.outputRepairUsed {
		return empty, modelgateway.ErrOutputRepair
	}
	route, ordinal, ok := h.route(op)
	if !ok || ordinal >= len(h.proof.routes) || req.OutputMode != modelgateway.Structured || !modelRunRequestEqual(req, route.preview.Request) {
		return empty, modelegressbudget.ErrDenied
	}
	next := h.proof.routes[ordinal]
	// RunID is the binding, not the operation; the complete original request
	// must be byte-identical. There is no repaired prompt or fallback egress.
	if next.binding.Destination != route.binding.Destination || !modelRunRequestEqual(next.preview.Request, req) {
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
		h.outputRepairUsed = true
	}
	return cp, e
}
