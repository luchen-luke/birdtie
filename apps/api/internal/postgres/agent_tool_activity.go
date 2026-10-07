package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"reflect"
	"sync"
	"time"
)

type nativeToolPlan struct {
	h     *nativeModelRunHandle
	calls int
}
type nativeToolCall struct {
	mu       sync.Mutex
	plan     *nativeToolPlan
	proposal agentplanner.ActionProposal
	policy   string
	decision agenttool.Decision
	bound    modelrequestrun.ClockBound
	used     bool
}

var _ agenttool.NativePlanFactory = (*nativeModelRunHandle)(nil)

func (h *nativeModelRunHandle) NativeToolPlan() agenttool.Plan {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.planner == nil || h.plannerView == nil || h.outputScope == nil {
		return nil
	}
	if h.toolPlan == nil {
		h.toolPlan = &nativeToolPlan{h: h}
	}
	return h.toolPlan
}
func (*nativeToolPlan) MarshalJSON() ([]byte, error) { return nil, agenttool.ErrServerOnly }
func (*nativeToolCall) MarshalJSON() ([]byte, error) { return nil, agenttool.ErrServerOnly }
func (p *nativeToolPlan) Check(ctx context.Context, a agentevent.Access, proposal agentplanner.ActionProposal, c *agentfeature.Controller) (agenttool.Decision, agenttool.Call, error) {
	if p == nil || p.h == nil || p.h.planner == nil || ctx == nil {
		return agenttool.Decision{}, nil, agenttool.ErrInvalid
	}
	h := p.h
	// Reuse the original native elapsed bound for lock waits as well as release.
	// A blocked policy/account transaction cannot extend this plan's lifetime.
	remaining := h.planner.Remaining(time.Now())
	if remaining <= 0 {
		return agenttool.Decision{SchemaVersion: agenttool.Schema, Disposition: agenttool.Deny, ReasonCodes: []string{"PLAN_LIMIT"}}, nil, agenttool.ErrLimit
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	h.mu.Lock()
	defer h.mu.Unlock()
	deny := func(code string, e error) (agenttool.Decision, agenttool.Call, error) {
		return agenttool.Decision{SchemaVersion: agenttool.Schema, Disposition: agenttool.Deny, ReasonCodes: []string{code}}, nil, e
	}
	descriptor, known := agenttool.Lookup(proposal.Tool)
	if !known || descriptor.Kind != "READ" {
		return deny("UNKNOWN_OR_NONREAD_TOOL", agenttool.ErrDenied)
	}
	if !proposal.Valid() || h.planner == nil || h.plannerView == nil || h.outputScope == nil || h.toolPlan != p {
		return deny("NATIVE_PLAN_REQUIRED", agenttool.ErrDenied)
	}
	matched := false
	for _, original := range h.plannerView.Actions {
		if reflect.DeepEqual(original, proposal) {
			matched = true
			break
		}
	}
	if !matched {
		return deny("EXACT_PROPOSAL_REQUIRED", agenttool.ErrDenied)
	}
	if p.calls >= agentplanner.MaxSteps || h.planner.Remaining(time.Now()) <= 0 {
		return deny("PLAN_LIMIT", agenttool.ErrLimit)
	}
	started := time.Now()
	tx, r, e := h.begin(ctx, a, c, true)
	if e != nil {
		return deny("CURRENT_AUTHORITY_REQUIRED", agenttool.ErrDenied)
	}
	defer tx.Rollback(ctx)
	if r.control.State != modelrequestrun.RunFinished {
		return deny("FINISHED_NATIVE_PLAN_REQUIRED", agenttool.ErrDenied)
	}
	policy, e := captureNativeToolPolicy(ctx, tx, agentPrivateBinding{h.proof.session, h.proof.owner, h.planner.agent}, h.planner.view.ValidUntil)
	if e != nil {
		return deny("POLICY_UNAVAILABLE_OR_EXPIRED", e)
	}
	limit := agenttool.Restrict(proposal.Tool, actorref.Person, policy.level)
	if limit.Denied {
		return deny(limit.Reason, agenttool.ErrDenied)
	}
	h.activeToolPolicy = policy
	defer func() { h.activeToolPolicy = nil }()
	cp, e := h.commit(ctx, tx, r, h.proof.routes[0].binding.Input.OperationID, true)
	if e != nil {
		return deny("SOURCE_OR_POLICY_CHANGED", agenttool.ErrChanged)
	}
	policy.until = retryMinimum(policy.until, cp.ValidUntil())
	d := nativeToolDecision(proposal.Tool, proposal.ActionID, proposal.LogicalOperationID, proposal.ResourceVersion, agenttool.Digest(proposal), policy.owner, policy.agent, policy, agenttool.Allow, "CURRENT_NATIVE_PUBLIC_ACTIVITY_READ")
	bound, e := modelrequestrun.NewClockBound(policy.observed, policy.until, started)
	if e != nil || bound.Remaining(time.Now()) <= 0 || ctx.Err() != nil {
		return deny("EXPIRED", agenttool.ErrChanged)
	}
	call := &nativeToolCall{plan: p, proposal: proposal, policy: policy.token, decision: agenttool.Clone(d), bound: bound}
	return agenttool.Clone(d), call, nil
}
func (call *nativeToolCall) Read(ctx context.Context, a agentevent.Access, c *agentfeature.Controller) (agenttool.Result, error) {
	if call == nil || ctx == nil || call.plan == nil || call.plan.h == nil {
		return agenttool.Result{}, agenttool.ErrInvalid
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	if call.used || call.bound.Remaining(time.Now()) <= 0 {
		return agenttool.Result{}, agenttool.ErrChanged
	}
	call.used = true // No automatic retry, cached authority or result reconstruction.
	p := call.plan
	h := p.h
	// Policy/session expiry is still checked in the final native payload SQL.
	// The wait ceiling uses the original plan, without extending either bound.
	ctx, cancel := context.WithTimeout(ctx, h.planner.Remaining(time.Now()))
	defer cancel()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.toolPlan != p || p.calls >= agentplanner.MaxSteps || h.planner.Remaining(time.Now()) <= 0 {
		return agenttool.Result{}, agenttool.ErrLimit
	}
	p.calls++ // One original bounded plan; a second factory call cannot reset it.
	tx, r, e := h.begin(ctx, a, c, true)
	if e != nil {
		return agenttool.Result{}, agenttool.ErrDenied
	}
	defer tx.Rollback(ctx)
	if r.control.State != modelrequestrun.RunFinished {
		return agenttool.Result{}, agenttool.ErrDenied
	}
	policy, e := captureNativeToolPolicy(ctx, tx, agentPrivateBinding{h.proof.session, h.proof.owner, h.planner.agent}, call.decision.ExpiresAt)
	if e != nil {
		return agenttool.Result{}, e
	}
	if policy.token != call.policy || agenttool.Restrict(call.proposal.Tool, actorref.Person, policy.level).Denied {
		return agenttool.Result{}, agenttool.ErrChanged
	}
	h.activeToolPolicy = policy
	defer func() { h.activeToolPolicy = nil }()
	scope := h.outputScope
	f := &modelOutputFence{id: h.id, owner: h.proof.owner, session: h.proof.session, fence: h.fence, finished: true, toolPolicy: policy}
	current, e := h.store.captureAgentResultProjectionTx(ctx, tx, scope.access, scope.query, f)
	if e != nil || !unchangedOutputProjection(scope.initial, current) {
		return agenttool.Result{}, agenttool.ErrChanged
	}
	activities := []foundation.Activity{}
	allowed := map[string]bool{}
	for _, ref := range current.PublicCommercialRefs {
		if ref.Type == "activity" {
			allowed[ref.ID] = true
		}
	}
	for _, activity := range current.Activities {
		if allowed[activity.ID] && (call.proposal.Tool == agentplanner.ActivitySearch || activity.ID == call.proposal.Arguments.ActivityID) {
			activities = append(activities, activity)
		}
	}
	if call.proposal.Tool == agentplanner.ActivityDetail && len(activities) != 1 {
		return agenttool.Result{}, agenttool.ErrDenied
	}
	cp, e := h.commit(ctx, tx, r, h.proof.routes[0].binding.Input.OperationID, true)
	if e != nil || ctx.Err() != nil || call.bound.Remaining(time.Now()) <= 0 {
		return agenttool.Result{}, agenttool.ErrChanged
	}
	return agenttool.Result{SchemaVersion: agenttool.ResultSchema, DecisionID: call.decision.DecisionID, ActionID: call.proposal.ActionID, Tool: call.proposal.Tool, Activities: activities, ObservedAt: current.ObservedAt, ValidUntil: retryMinimum(policy.until, cp.ValidUntil())}, nil
}
