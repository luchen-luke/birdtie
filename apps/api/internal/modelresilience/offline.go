package modelresilience

import (
	"context"
	"errors"
	"math/rand/v2"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// OfflineResolver supplies synthetic current facts for a local contract suite.
// Its time argument is the exact check instant expected by AIR008. It cannot
// be injected into Service and is not a native source-purpose/egress resolver.
type OfflineResolver interface {
	ResolveOffline(context.Context, modelgateway.Request, modelcapability.Requirements, time.Time) (modelcapability.OfflineGrant, error)
}

type Attempt struct {
	Number                   int
	Destination              modelcapability.Key
	Region                   modelcapability.Region
	ReasonCode               string
	ScheduledWait            time.Duration
	ConfigurationFingerprint string
}

type Outcome struct {
	Result     modelgateway.Result
	Attempts   []Attempt
	ReasonCode string
}

type OfflineRunner struct {
	registry       *modelcapability.Registry
	configurations *modelconfiguration.Registry
	adapters       []modelcapability.OfflineAdapter
	resolver       OfflineResolver
	policy         Policy
	now            func() time.Time
	wait           func(context.Context, time.Duration) error
	jitter         func() int
}

func present(v any) bool {
	if v == nil {
		return false
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return !r.IsNil()
	}
	return true
}

func NewOfflineRunner(registry *modelcapability.Registry, adapters []modelcapability.OfflineAdapter, configurations *modelconfiguration.Registry, resolver OfflineResolver, policy Policy) (*OfflineRunner, error) {
	if registry == nil || configurations == nil || !present(resolver) || ValidatePolicy(policy) != nil {
		return nil, ErrInvalid
	}
	if _, err := modelcapability.NewOfflineRunner(registry, adapters); err != nil {
		return nil, ErrUnavailable
	}
	return &OfflineRunner{registry: registry, configurations: configurations, adapters: append([]modelcapability.OfflineAdapter{}, adapters...),
		resolver: resolver, policy: policy, now: time.Now, wait: waitContext, jitter: func() int { return rand.IntN(1001) }}, nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func copyRequest(r modelgateway.Request) modelgateway.Request {
	r.Messages = append([]modelgateway.Message{}, r.Messages...)
	r.ToolAllowlist = append([]string{}, r.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string{}, r.CapabilitiesRequired...)
	return r
}
func copyGrant(g modelcapability.OfflineGrant) modelcapability.OfflineGrant {
	g.Destinations = append([]modelcapability.Destination{}, g.Destinations...)
	return g
}
func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
func empty(r modelgateway.Request, reason string) modelgateway.Result {
	return modelgateway.Result{SchemaVersion: modelgateway.ResultVersion, RunID: r.RunID, Agent: r.Agent, Status: modelgateway.Unavailable,
		Mode: modelgateway.OfflineContract, Usage: modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}, FinishReason: "none", ReasonCode: reason}
}

func sameAuthority(g, initial modelcapability.OfflineGrant) bool {
	return g.Agent == initial.Agent && g.RequestDigest == initial.RequestDigest && g.RegistryDigest == initial.RegistryDigest &&
		g.SourceVersion == initial.SourceVersion && g.CurrentSourceVersion == initial.CurrentSourceVersion && g.Purpose == initial.Purpose &&
		g.ConsentRevision == initial.ConsentRevision && g.CurrentConsentRevision == initial.CurrentConsentRevision &&
		g.PolicyRevision == initial.PolicyRevision && g.CurrentPolicyRevision == initial.CurrentPolicyRevision
}

// restrict never refreshes an old grant's checkedAt. A newly resolved grant is
// limited to the initial exact destinations/region, retained authority versions
// and expiry. State/storage permission may shrink but cannot grow.
func restrict(g, initial modelcapability.OfflineGrant, excluded map[modelcapability.Key]bool) (modelcapability.OfflineGrant, error) {
	if !sameAuthority(g, initial) {
		return modelcapability.OfflineGrant{}, ErrDenied
	}
	g = copyGrant(g)
	g.ExpiresAt = earlier(g.ExpiresAt, initial.ExpiresAt)
	var narrowed []modelcapability.Destination
	for _, current := range g.Destinations {
		for _, original := range initial.Destinations {
			if current.Key == original.Key && current.Region == original.Region && !excluded[current.Key] {
				current.AllowState = current.AllowState && original.AllowState
				current.AllowStorage = current.AllowStorage && original.AllowStorage
				narrowed = append(narrowed, current)
			}
		}
	}
	if len(narrowed) == 0 {
		return modelcapability.OfflineGrant{}, ErrUnavailable
	}
	g.Destinations = narrowed
	return g, nil
}

func capabilityError(err error) error {
	if errors.Is(err, modelcapability.ErrDenied) || errors.Is(err, modelcapability.ErrExpired) {
		return ErrDenied
	}
	return ErrUnavailable
}

// AIR008's exact checkedAt belongs to a synthetic snapshot. Resolver latency
// cannot keep its clock current: check actual time again without rewriting the
// grant's checkedAt or minting fresh permission from the old snapshot.
func (runner *OfflineRunner) clockAllows(request modelgateway.Request, grant modelcapability.OfflineGrant, plan modelcapability.Plan, at time.Time) bool {
	if at.IsZero() || at.Before(grant.CheckedAt) || !grant.ExpiresAt.After(at) || modelgateway.ValidateRequest(request, at) != nil {
		return false
	}
	for _, record := range runner.registry.Records() {
		if record.Key == plan.Key {
			return !record.ValidatedAt.After(at) && record.ExpiresAt.After(at)
		}
	}
	return false
}

// Complete is a real callable OFFLINE_CONTRACT retry path. It pins the central
// immutable configuration, calls AIR008/AIR007, and re-resolves synthetic facts
// before each attempt and before releasing any response. No tool is executed.
// Real Run persistence and AIR011 spend reservations are deliberately absent.
func (runner *OfflineRunner) Complete(ctx context.Context, request modelgateway.Request, needs modelcapability.Requirements, reference modelconfiguration.RunReference) (Outcome, error) {
	out := Outcome{}
	finish := func(reason string, err error) (Outcome, error) {
		out.ReasonCode, out.Result = reason, empty(request, reason)
		return out, err
	}
	if runner == nil || runner.now == nil || runner.wait == nil || runner.jitter == nil || !present(runner.resolver) || ctx == nil {
		return finish("invalid_runner", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return finish("cancelled", err)
	}
	request = copyRequest(request)
	reference = modelconfiguration.CloneReference(reference)
	started := runner.now()
	configuration, err := runner.configurations.ResolveReference(reference)
	if err != nil {
		return finish("configuration_unavailable", ErrUnavailable)
	}
	bound, err := modelconfiguration.BindRequest(configuration, request, started)
	if err != nil || !reflect.DeepEqual(bound, reference) {
		return finish("configuration_mismatch", ErrDenied)
	}
	if needs.Vision || needs.Streaming || needs.State || needs.Storage {
		return finish("unsupported_capability", ErrUnavailable)
	}
	deadline := earlier(request.DeadlineAt, started.Add(runner.policy.MaxElapsed))
	if d, ok := ctx.Deadline(); ok {
		deadline = earlier(deadline, d)
	}
	runContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var initial modelcapability.OfflineGrant
	excluded := map[modelcapability.Key]bool{}
	var currentKey modelcapability.Key
	routeCalls, switches := 0, 0
	for number := 1; number <= runner.policy.MaxAttempts; number++ {
		if err := runContext.Err(); err != nil {
			return finish("deadline_or_cancelled", err)
		}
		checkedAt := runner.now()
		if !checkedAt.Before(deadline) {
			return finish("deadline_exhausted", ErrDeadline)
		}
		grant, err := runner.resolver.ResolveOffline(runContext, copyRequest(request), needs, checkedAt)
		if err != nil {
			return finish("current_permission_unavailable", ErrUnavailable)
		}
		grant = copyGrant(grant)
		if err := runContext.Err(); err != nil {
			return finish("deadline_or_cancelled", err)
		}
		if number > 1 {
			grant, err = restrict(grant, initial, excluded)
			if err != nil {
				return finish("original_permission_required", err)
			}
		}
		plan, err := modelcapability.SelectOffline(runner.registry, request, needs, grant, checkedAt)
		if err != nil {
			return finish("no_authorized_route", capabilityError(err))
		}
		if number == 1 {
			initial = copyGrant(grant)
			deadline = earlier(deadline, initial.ExpiresAt)
		}
		if !runner.clockAllows(request, grant, plan, runner.now()) {
			return finish("expired_before_dispatch", ErrDenied)
		}
		deadline = earlier(deadline, grant.ExpiresAt)
		if currentKey != (modelcapability.Key{}) && plan.Key != currentKey {
			switches++
			if switches > runner.policy.MaxProviderSwitches {
				return finish("route_switch_budget_exhausted", ErrBudget)
			}
			routeCalls = 0
		}
		currentKey = plan.Key
		routeCalls++
		// Lock this exact plan into AIR008 rather than disguising a different
		// provider inside the original adapter/descriptor.
		var exact []modelcapability.Destination
		for _, d := range grant.Destinations {
			if d.Key == plan.Key && d.Region == plan.Region {
				exact = append(exact, d)
			}
		}
		grant.Destinations = exact
		capabilityRunner, err := modelcapability.NewOfflineRunnerWithClock(runner.registry, runner.adapters, func() time.Time { return checkedAt })
		if err != nil {
			return finish("offline_adapter_unavailable", ErrUnavailable)
		}
		attemptDeadline := earlier(deadline, checkedAt.Add(runner.policy.AttemptTimeout))
		attemptContext, stop := context.WithDeadline(runContext, attemptDeadline)
		out.Attempts = append(out.Attempts, Attempt{Number: number, Destination: plan.Key, Region: plan.Region, ConfigurationFingerprint: reference.ConfigurationFingerprint})
		result, callErr := capabilityRunner.Complete(attemptContext, copyRequest(request), needs, grant)
		attemptExpired := attemptContext.Err()
		stop()
		if err := runContext.Err(); err != nil {
			return finish("deadline_or_cancelled", err)
		}
		if attemptExpired != nil || !runner.now().Before(attemptDeadline) {
			out.Attempts[len(out.Attempts)-1].ReasonCode = "deadline_unknown_outcome"
			return finish("deadline_unknown_outcome", ErrDeadline)
		}
		// Do not release even a refusal/partial result after changed source or
		// permission. This recheck is synthetic, not a native atomic resolver.
		finalAt := runner.now()
		current, resolveErr := runner.resolver.ResolveOffline(runContext, copyRequest(request), needs, finalAt)
		if resolveErr != nil {
			return finish("current_permission_unavailable", ErrUnavailable)
		}
		current, err = restrict(current, initial, nil)
		if err != nil {
			return finish("permission_changed", err)
		}
		var chosen []modelcapability.Destination
		for _, d := range current.Destinations {
			if d.Key == plan.Key && d.Region == plan.Region {
				chosen = append(chosen, d)
			}
		}
		current.Destinations = chosen
		if _, err = modelcapability.SelectOffline(runner.registry, request, needs, current, finalAt); err != nil {
			return finish("permission_changed", capabilityError(err))
		}
		if !runner.clockAllows(request, current, plan, runner.now()) {
			return finish("expired_before_release", ErrDenied)
		}
		deadline = earlier(deadline, current.ExpiresAt)
		if err := runContext.Err(); err != nil {
			return finish("deadline_or_cancelled", err)
		}
		if !runner.now().Before(deadline) {
			return finish("deadline_exhausted", ErrDeadline)
		}
		if callErr == nil {
			out.Attempts[len(out.Attempts)-1].ReasonCode = string(result.Status)
			out.Result, out.ReasonCode = result, "offline_terminal_response"
			return out, nil
		}
		var provider modelgateway.ProviderError
		if !errors.As(callErr, &provider) || !provider.Retryable || (provider.Code != "RATE_LIMIT" && provider.Code != "TEMPORARY") {
			out.Attempts[len(out.Attempts)-1].ReasonCode = "terminal_provider_failure"
			return finish("terminal_provider_failure", ErrUnavailable)
		}
		out.Attempts[len(out.Attempts)-1].ReasonCode = provider.Code
		if number == runner.policy.MaxAttempts {
			return finish("attempt_budget_exhausted", ErrBudget)
		}
		if routeCalls >= runner.policy.SameRouteAttempts && runner.policy.MaxProviderSwitches > switches {
			excluded[plan.Key] = true
		}
		hint, hasHint, valid := modelgateway.RetryAfter(callErr)
		if hasHint && !valid {
			return finish("invalid_retry_after", ErrUnavailable)
		}
		delay, err := RetryDelay(runner.policy, number, hint, hasHint, runner.jitter())
		if err != nil {
			return finish("retry_wait_budget_exhausted", err)
		}
		if !runner.now().Add(delay).Before(deadline) {
			return finish("retry_wait_deadline_exhausted", ErrBudget)
		}
		out.Attempts[len(out.Attempts)-1].ScheduledWait = delay
		if err := runner.wait(runContext, delay); err != nil {
			if runContext.Err() != nil {
				return finish("deadline_or_cancelled", runContext.Err())
			}
			return finish("retry_wait_unavailable", ErrUnavailable)
		}
	}
	return finish("attempt_budget_exhausted", ErrBudget)
}
