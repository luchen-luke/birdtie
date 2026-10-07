package modelegressbudget

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
)

// The handle is a native, process-local association, not reconstructible from
// Control JSON. Current source permissions and the original ticket still apply.
type LocalModelRunHandle interface {
	LocalRetryPort
	StopOwnLocalModelRun(context.Context, agentevent.Access) error
}
type LocalModelRunPort interface {
	CreateOwnLocalModelRun(context.Context, agentevent.Access, string, []LocalRetryBinding, *agentfeature.Controller, agentfeature.Ticket) (LocalModelRunHandle, modelrequestrun.Control, error)
	ReadOwnLocalModelRun(context.Context, agentevent.Access, string) (modelrequestrun.Control, error)
	CancelOwnLocalModelRun(context.Context, agentevent.Access, string, int64) (modelrequestrun.Control, error)
}
type LocalModelRunRunner struct {
	port   LocalModelRunPort
	gate   *agentfeature.Controller
	policy modelresilience.Policy
	jitter func() int
}

func NewLocalModelRunRunner(port LocalModelRunPort, gate *agentfeature.Controller, policy modelresilience.Policy) *LocalModelRunRunner {
	if port == nil || gate == nil || modelresilience.ValidatePolicy(policy) != nil {
		return nil
	}
	v := reflect.ValueOf(port)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
		if v.IsNil() {
			return nil
		}
	}
	return &LocalModelRunRunner{port: port, gate: gate, policy: policy, jitter: func() int { return rand.IntN(1001) }}
}

// Run accepts only concrete drivers made with this same native port. Planned
// fallback is metadata: no reservation or request is made until its actual turn.
func (r *LocalModelRunRunner) Run(ctx context.Context, a agentevent.Access, id string, steps []LocalRetryStep) (LocalRetryOutcome, error) {
	return r.run(ctx, a, id, steps, false)
}

// RunValidated adds explicit output failure/one format repair to the same
// original ModelRun and budget steps. Provider input remains query-only.
func (r *LocalModelRunRunner) RunValidated(ctx context.Context, a agentevent.Access, id string, steps []LocalRetryStep) (LocalRetryOutcome, error) {
	return r.run(ctx, a, id, steps, true)
}
func (r *LocalModelRunRunner) run(ctx context.Context, a agentevent.Access, id string, steps []LocalRetryStep, validate bool) (LocalRetryOutcome, error) {
	var out LocalRetryOutcome
	if r == nil || ctx == nil || len(steps) < 1 || len(steps) > r.policy.MaxAttempts {
		return out, ErrInvalid
	}
	started := time.Now()
	deadline := started.Add(r.policy.MaxElapsed)
	runctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ticket, e := r.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		return out, ErrUnavailable
	}
	return r.runAt(runctx, a, id, steps, validate, nil, ticket, deadline)
}

// Planner uses this same original driver with an already captured ticket and
// elapsed deadline. It never creates a second retry loop or budget ledger.
func (r *LocalModelRunRunner) runAt(runctx context.Context, a agentevent.Access, id string, steps []LocalRetryStep, validate bool, goal agentplanner.PreparedGoal, ticket agentfeature.Ticket, deadline time.Time) (LocalRetryOutcome, error) {
	var out LocalRetryOutcome
	var e error
	bindings := make([]LocalRetryBinding, len(steps))
	copied := append([]LocalRetryStep(nil), steps...)
	for i, step := range copied {
		if step.Driver == nil || step.Driver.gate != r.gate || !sameLocalPort(step.Driver.port, r.port) || !step.Driver.adapter.current() {
			return out, ErrInvalid
		}
		bindings[i] = LocalRetryBinding{step.Input, step.Driver.adapter.destination}
	}
	if !r.gate.Current(ticket) || runctx.Err() != nil {
		return out, ErrUnavailable
	}
	var handle LocalModelRunHandle
	if goal == nil {
		handle, _, e = r.port.CreateOwnLocalModelRun(runctx, a, id, bindings, r.gate, ticket)
	} else {
		port, ok := r.port.(LocalPlannerPort)
		if !ok {
			return out, ErrUnavailable
		}
		handle, _, e = port.CreateOwnLocalPlannerRun(runctx, a, id, bindings, r.gate, ticket, goal)
	}
	if e != nil {
		return out, e
	}
	if handle == nil {
		return out, ErrUnavailable
	}
	if validate {
		if _, ok := handle.(LocalOutputPort); !ok {
			return out, ErrUnavailable
		}
	}
	for i := range copied {
		driver := *copied[i].Driver
		driver.port = handle
		driver.validateOutput = validate
		copied[i].Driver = &driver
	}
	retry := &LocalRetryRunner{port: handle, gate: r.gate, policy: r.policy, jitter: r.jitter, outputValidation: validate}
	out, e = retry.runWithTicket(runctx, a, copied, ticket, deadline)
	if e == nil && goal != nil {
		p, ok := handle.(LocalPlannerHandle)
		if !ok {
			e = ErrUnavailable
		} else {
			var view agentplanner.View
			var cp LocalReleaseCheckpoint
			view, cp, e = p.ReadOwnReadonlyPlan(runctx, a, r.gate)
			if e == nil {
				if runctx.Err() != nil || !r.gate.Current(ticket) || cp.Remaining(time.Now()) <= 0 || !view.ValidElapsed(deadline.Add(-r.policy.MaxElapsed), time.Now()) {
					e = agentplanner.ErrChanged
				} else {
					out.EncodedResult, e = json.Marshal(view)
					if e == nil {
						if factory, ok := handle.(agenttool.NativePlanFactory); ok {
							out.toolPlan = factory.NativeToolPlan()
						}
					}
				}
			}
		}
		if e != nil {
			out.EncodedResult = nil
		}
	}
	if e != nil {
		// A stopping control does not refund, settle, or guess dispatch status.
		// The original error/unknown remains visible even if stop is unavailable.
		stopctx, stopcancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = handle.StopOwnLocalModelRun(stopctx, a)
		stopcancel()
	}
	return out, e
}
