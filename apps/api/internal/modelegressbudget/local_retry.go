package modelegressbudget

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
)

// LocalRetryBinding names already approved original records. It is not a grant
// and never creates/approves another preview or changes an operation ID.
type LocalRetryBinding struct {
	Input       ReserveInput
	Destination LocalAttemptDestination
}
type LocalRetryProof interface{ Remaining(time.Time) time.Duration }
type LocalRetryPort interface {
	LocalAttemptPort
	CaptureOwnLocalRetryPlan(context.Context, agentevent.Access, []LocalRetryBinding, *agentfeature.Controller) (LocalRetryProof, error)
	RevalidateOwnLocalRetryPlan(context.Context, agentevent.Access, LocalRetryProof, *agentfeature.Controller) (LocalReleaseCheckpoint, error)
	CheckOwnLocalRetryFailure(context.Context, agentevent.Access, LocalRetryProof, LocalRetryFailure, *agentfeature.Controller) (LocalReleaseCheckpoint, error)
	CheckOwnLocalRetryDispatch(context.Context, agentevent.Access, LocalRetryProof, string, modelgateway.Request, LocalAttemptDestination, *agentfeature.Controller) (LocalReleaseCheckpoint, error)
}

// This fact is signed only inside onceWithTicket after an actual whitelisted
// adapter response and confirmed accounting. It is not continued permission:
// the native port still checks the original approved plan and settled operation.
type LocalRetryFailure struct {
	operation   string
	request     modelgateway.Request
	requestHash [32]byte
	code        string
	delay       time.Duration
	hasDelay    bool
}

func newLocalRetryFailure(operation string, request modelgateway.Request, code string, delay time.Duration, present bool) *LocalRetryFailure {
	if operation == "" || (code != "RATE_LIMIT" && code != "TEMPORARY") || modelgateway.ValidateRequest(request, time.Now()) != nil {
		return nil
	}
	wire, e := json.Marshal(request)
	if e != nil {
		return nil
	}
	var clone modelgateway.Request
	if json.Unmarshal(wire, &clone) != nil {
		return nil
	}
	return &LocalRetryFailure{operation: operation, request: clone, requestHash: sha256.Sum256(wire), code: code, delay: delay, hasDelay: present}
}
func (f LocalRetryFailure) OperationRequest() (string, modelgateway.Request, bool) {
	wire, e := json.Marshal(f.request)
	if e != nil || f.operation == "" || (f.code != "RATE_LIMIT" && f.code != "TEMPORARY") || sha256.Sum256(wire) != f.requestHash {
		return "", modelgateway.Request{}, false
	}
	var clone modelgateway.Request
	if json.Unmarshal(wire, &clone) != nil {
		return "", clone, false
	}
	return f.operation, clone, true
}
func (LocalRetryFailure) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (f *LocalRetryFailure) UnmarshalJSON([]byte) error {
	*f = LocalRetryFailure{}
	return ErrServerOnly
}

type LocalRetryStep struct {
	Driver *LocalAttemptDriver
	Input  ReserveInput
}
type LocalRetryOutcome struct {
	Attempts      []AttemptControl
	EncodedResult []byte
	ReasonCode    string
	toolPlan      agenttool.Plan
}

func (LocalRetryOutcome) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (o *LocalRetryOutcome) UnmarshalJSON([]byte) error {
	*o = LocalRetryOutcome{}
	return ErrServerOnly
}

type LocalRetryRunner struct {
	port             LocalRetryPort
	gate             *agentfeature.Controller
	policy           modelresilience.Policy
	jitter           func() int
	outputValidation bool
}

func NewLocalRetryRunner(port LocalRetryPort, gate *agentfeature.Controller, policy modelresilience.Policy) *LocalRetryRunner {
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
	return &LocalRetryRunner{port: port, gate: gate, policy: policy, jitter: func() int { return rand.IntN(1001) }}
}
func sameLocalPort(a, b any) bool {
	if a == nil || b == nil || reflect.TypeOf(a) != reflect.TypeOf(b) || !reflect.TypeOf(a).Comparable() {
		return false
	}
	return a == b
}

// Run performs bounded local synthetic retries only. The entire original A/B
// set must be currently approved before the first reservation, and one original
// gate ticket/deadline is retained throughout. Service/main/HTTP remain OFF.
func (r *LocalRetryRunner) Run(ctx context.Context, a agentevent.Access, steps []LocalRetryStep) (LocalRetryOutcome, error) {
	var out LocalRetryOutcome
	if r == nil || ctx == nil || len(steps) < 1 || len(steps) > r.policy.MaxAttempts {
		return out, ErrInvalid
	}
	// Capture the whole-round switch and monotonic elapsed bound before any
	// adapter getter or native pool/source capture can wait or change the gate.
	started := time.Now()
	deadline := started.Add(r.policy.MaxElapsed)
	runContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ticket, e := r.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		return out, ErrUnavailable
	}
	return r.runWithTicket(runContext, a, steps, ticket, deadline)
}

// The native ModelRequestRun entry carries the initial ticket and elapsed
// deadline here; an association must never recapture a replacement ticket.
func (r *LocalRetryRunner) runWithTicket(runContext context.Context, a agentevent.Access, steps []LocalRetryStep, ticket agentfeature.Ticket, deadline time.Time) (LocalRetryOutcome, error) {
	var out LocalRetryOutcome
	steps = append([]LocalRetryStep(nil), steps...)
	bindings := make([]LocalRetryBinding, len(steps))
	seen := map[string]bool{}
	routeCalls, switches := 0, 0
	for i, step := range steps {
		if step.Driver == nil || step.Driver.gate != r.gate || !sameLocalPort(step.Driver.port, r.port) || step.Input.OperationID == "" || seen[step.Input.OperationID] || !step.Driver.adapter.current() {
			return out, ErrInvalid
		}
		seen[step.Input.OperationID] = true
		if i > 0 && (step.Input.RootTraceID != steps[0].Input.RootTraceID || step.Input.TaskID != steps[0].Input.TaskID) {
			return out, ErrDenied
		}
		bindings[i] = LocalRetryBinding{step.Input, step.Driver.adapter.destination}
		if i == 0 || bindings[i].Destination == bindings[i-1].Destination {
			routeCalls++
		} else {
			routeCalls = 1
			switches++
		}
		if (routeCalls > r.policy.SameRouteAttempts && switches < r.policy.MaxProviderSwitches) || switches > r.policy.MaxProviderSwitches {
			return out, ErrInvalid
		}
	}
	if runContext.Err() != nil {
		return out, runContext.Err()
	}
	if !r.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	proof, e := r.port.CaptureOwnLocalRetryPlan(runContext, a, bindings, r.gate)
	if e != nil {
		return out, e
	}
	if proof == nil || !r.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	now := time.Now()
	remaining := proof.Remaining(now)
	if remaining <= 0 {
		return out, ErrDenied
	}
	if sourceDeadline := now.Add(remaining); sourceDeadline.Before(deadline) {
		deadline = sourceDeadline
	}
	repairs := 0
	for i, step := range steps {
		if runContext.Err() != nil {
			return out, runContext.Err()
		}
		if !r.gate.Current(ticket) || !time.Now().Before(deadline) {
			return out, ErrUnavailable
		}
		checkpoint, e := r.port.RevalidateOwnLocalRetryPlan(runContext, a, proof, r.gate)
		if e != nil {
			return out, e
		}
		if !r.gate.Current(ticket) || !checkpoint.ValidAt(time.Now()) || proof.Remaining(time.Now()) <= 0 {
			return out, ErrDenied
		}
		now = time.Now()
		if sourceDeadline := now.Add(checkpoint.Remaining(now)); sourceDeadline.Before(deadline) {
			deadline = sourceDeadline
		}
		attemptDeadline := time.Now().Add(r.policy.AttemptTimeout)
		if deadline.Before(attemptDeadline) {
			attemptDeadline = deadline
		}
		attemptContext, attemptCancel := context.WithDeadline(runContext, attemptDeadline)
		dispatchCheck := func(ctx context.Context, a agentevent.Access, operation string, g *agentfeature.Controller, request modelgateway.Request, destination LocalAttemptDestination) (LocalReleaseCheckpoint, error) {
			return r.port.CheckOwnLocalRetryDispatch(ctx, a, proof, operation, request, destination, g)
		}
		result, callErr := step.Driver.onceWithTicket(attemptContext, a, step.Input, ticket, dispatchCheck)
		attemptCancel()
		// A shorter bound learned inside beforeCall/release persists for the
		// whole original round, even if a later legal idle refresh is longer.
		// This private clock metadata never replaces the native permission check.
		if !result.monotonicDeadline.IsZero() && result.monotonicDeadline.Before(deadline) {
			deadline = result.monotonicDeadline
		}
		if result.Control.OperationID != "" {
			out.Attempts = append(out.Attempts, result.Control)
		}
		if callErr == nil {
			encoded := append([]byte(nil), result.EncodedResult...)
			checkpoint, e = r.port.RevalidateOwnLocalRetryPlan(runContext, a, proof, r.gate)
			if e != nil {
				return out, e
			}
			if runContext.Err() != nil {
				return out, runContext.Err()
			}
			if !r.gate.Current(ticket) || !time.Now().Before(deadline) || !checkpoint.ValidAt(time.Now()) || proof.Remaining(time.Now()) <= 0 {
				return out, ErrDenied
			}
			out.EncodedResult = encoded
			out.ReasonCode = "local_terminal_result"
			return out, nil
		}
		if result.outputFailure != nil {
			port, ok := r.port.(LocalOutputPort)
			if !r.outputValidation || !ok || repairs >= 1 || i+1 == len(steps) {
				out.ReasonCode = "output_repair_exhausted"
				// Preserve only fixed, already normalized classifications. Never
				// join a provider exception or raw response into caller errors.
				if errors.Is(callErr, modelgateway.ErrOutputTruncated) {
					return out, errors.Join(modelgateway.ErrOutputRepair, modelgateway.ErrOutputTruncated)
				}
				if errors.Is(callErr, modelgateway.ErrOutputSchema) {
					return out, errors.Join(modelgateway.ErrOutputRepair, modelgateway.ErrOutputSchema)
				}
				return out, modelgateway.ErrOutputRepair
			}
			checkpoint, e = port.CheckOwnLocalOutputFailure(runContext, a, proof, *result.outputFailure, r.gate)
			if e != nil {
				return out, e
			}
			if runContext.Err() != nil || !r.gate.Current(ticket) || !checkpoint.ValidAt(time.Now()) || proof.Remaining(time.Now()) <= 0 {
				return out, ErrDenied
			}
			now = time.Now()
			if bound := now.Add(checkpoint.Remaining(now)); bound.Before(deadline) {
				deadline = bound
			}
			repairs++
			continue // Next immutable, independently approved step; no prompt rewrite.
		}
		if result.retryFailure == nil {
			out.ReasonCode = "unknown_or_terminal_stop"
			return out, callErr
		}
		if i+1 == len(steps) {
			out.ReasonCode = "attempt_budget_exhausted"
			return out, ErrBudget
		}
		checkpoint, e = r.port.CheckOwnLocalRetryFailure(runContext, a, proof, *result.retryFailure, r.gate)
		if e != nil {
			return out, e
		}
		if !r.gate.Current(ticket) || !checkpoint.ValidAt(time.Now()) || proof.Remaining(time.Now()) <= 0 {
			return out, ErrDenied
		}
		now = time.Now()
		if sourceDeadline := now.Add(checkpoint.Remaining(now)); sourceDeadline.Before(deadline) {
			deadline = sourceDeadline
		}
		delay, e := modelresilience.RetryDelay(r.policy, i+1, result.retryFailure.delay, result.retryFailure.hasDelay, r.jitter())
		if e != nil || !time.Now().Add(delay).Before(deadline) || delay >= checkpoint.Remaining(time.Now()) {
			return out, ErrBudget
		}
		timer := time.NewTimer(delay)
		select {
		case <-runContext.Done():
			timer.Stop()
			return out, runContext.Err()
		case <-timer.C:
		}
		// The same failure/plan is revalidated after waiting. Neither recovered
		// accounting nor a replacement/newer preview can authorize the next call.
		checkpoint, e = r.port.CheckOwnLocalRetryFailure(runContext, a, proof, *result.retryFailure, r.gate)
		if e != nil {
			return out, e
		}
		if !r.gate.Current(ticket) || !checkpoint.ValidAt(time.Now()) || proof.Remaining(time.Now()) <= 0 {
			return out, ErrDenied
		}
		now = time.Now()
		if sourceDeadline := now.Add(checkpoint.Remaining(now)); sourceDeadline.Before(deadline) {
			deadline = sourceDeadline
		}
	}
	return out, ErrBudget
}
