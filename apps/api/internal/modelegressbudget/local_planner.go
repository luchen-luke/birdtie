package modelegressbudget

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
	"math/rand/v2"
	"time"
)

type LocalPlannerHandle interface {
	LocalModelRunHandle
	ReadOwnReadonlyPlan(context.Context, agentevent.Access, *agentfeature.Controller) (agentplanner.View, LocalReleaseCheckpoint, error)
}
type LocalPlannerPort interface {
	LocalModelRunPort
	agentplanner.TaskPort
	CreateOwnLocalPlannerRun(context.Context, agentevent.Access, string, []LocalRetryBinding, *agentfeature.Controller, agentfeature.Ticket, agentplanner.PreparedGoal) (LocalModelRunHandle, modelrequestrun.Control, error)
}
type LocalPlannerOutcome struct {
	Plan     agentplanner.View
	Attempts []AttemptControl
	toolPlan agenttool.Plan
}

// ToolPlan exposes only the exact native server association retained by Run.
// Plan/Control JSON and a new process cannot reconstruct it.
func (o LocalPlannerOutcome) ToolPlan() agenttool.Plan { return o.toolPlan }

func (LocalPlannerOutcome) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (o *LocalPlannerOutcome) UnmarshalJSON([]byte) error {
	*o = LocalPlannerOutcome{}
	return ErrServerOnly
}

type LocalPlannerRunner struct {
	port   LocalPlannerPort
	gate   *agentfeature.Controller
	policy modelresilience.Policy
}

func DefaultPlannerPolicy() modelresilience.Policy {
	p := modelresilience.DefaultPolicy()
	p.MaxAttempts = agentplanner.MaxModelCalls
	p.MaxElapsed = agentplanner.MaxElapsed
	p.AttemptTimeout = 20 * time.Second
	p.SameRouteAttempts = 2
	p.MaxProviderSwitches = 0
	return p
}
func NewLocalPlannerRunner(port LocalPlannerPort, gate *agentfeature.Controller, policy modelresilience.Policy) *LocalPlannerRunner {
	if NewLocalModelRunRunner(port, gate, policy) == nil || policy.MaxAttempts > agentplanner.MaxModelCalls || policy.MaxElapsed > agentplanner.MaxElapsed {
		return nil
	}
	return &LocalPlannerRunner{port, gate, policy}
}

// Only current native goals are actionable here. All attempts, including repair,
// share one original ticket, deadline, ModelRun and four original budget scopes.
// The returned Plan has no executor and default Main/HTTP do not call this path.
func (r *LocalPlannerRunner) Run(ctx context.Context, a agentevent.Access, runID, taskID, operation string, steps []LocalRetryStep) (LocalPlannerOutcome, error) {
	var out LocalPlannerOutcome
	if r == nil || ctx == nil || !agentplanner.ValidID(runID) || !agentplanner.ValidID(taskID) || !agentplanner.ValidID(operation) {
		return out, agentplanner.ErrInvalid
	}
	if len(steps) > r.policy.MaxAttempts {
		return out, agentplanner.ErrLimit
	}
	started := time.Now()
	deadline := started.Add(r.policy.MaxElapsed)
	runctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ticket, gateErr := r.gate.Capture(agentfeature.Enrichment) // Before any native wait; never recaptured.
	goal, e := r.port.PrepareOwnReadonlyPlan(runctx, a, taskID, operation)
	if e != nil {
		return out, e
	}
	if goal == nil || goal.Remaining(time.Now()) <= 0 {
		return out, agentplanner.ErrChanged
	}
	if !goal.NeedsModel() {
		v := goal.View()
		if runctx.Err() != nil || goal.Remaining(time.Now()) <= 0 || !v.ValidElapsed(started, time.Now()) {
			return out, agentplanner.ErrChanged
		}
		out.Plan = v
		return out, nil
	}
	if gateErr != nil || !r.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	if len(steps) < 1 {
		return out, agentplanner.ErrInvalid
	}
	for _, s := range steps {
		if s.Input.TaskID != taskID {
			return out, agentplanner.ErrDenied
		}
	}
	retry := &LocalModelRunRunner{port: r.port, gate: r.gate, policy: r.policy, jitter: func() int { return rand.IntN(1001) }}
	actual, e := retry.runAt(runctx, a, runID, steps, true, goal, ticket, deadline)
	out.Attempts = actual.Attempts
	if e != nil {
		return out, e
	}
	// runAt returns only the actual handle's already-native-revalidated typed
	// projection, encoded separately from the normalized provider result.
	if json.Unmarshal(actual.EncodedResult, &out.Plan) != nil || !out.Plan.ValidElapsed(started, time.Now()) || runctx.Err() != nil || !r.gate.Current(ticket) {
		return LocalPlannerOutcome{Attempts: out.Attempts}, agentplanner.ErrChanged
	}
	out.toolPlan = actual.toolPlan
	return out, nil
}
