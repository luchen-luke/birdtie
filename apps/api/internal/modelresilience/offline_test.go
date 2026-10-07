package modelresilience

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

type resolverFunc func(context.Context, modelgateway.Request, modelcapability.Requirements, time.Time) (modelcapability.OfflineGrant, error)

func (f resolverFunc) ResolveOffline(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
	return f(c, r, n, at)
}

type fakeAdapter struct {
	mu       sync.Mutex
	key      modelcapability.Key
	calls    int
	times    []time.Time
	requests []modelgateway.ProviderRequest
	invoke   func(context.Context, int) ([]byte, error)
}

func (f *fakeAdapter) Descriptor() modelgateway.ProviderDescriptor {
	return modelgateway.ProviderDescriptor{ProviderID: f.key.Provider, ModelID: f.key.Model, ModelVersion: f.key.Version, Mode: modelgateway.OfflineContract}
}
func (f *fakeAdapter) WireContractVersion() string { return f.key.WireContract }
func (f *fakeAdapter) Complete(ctx context.Context, r modelgateway.ProviderRequest) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	number := f.calls
	f.times = append(f.times, time.Now())
	f.requests = append(f.requests, r)
	hook := f.invoke
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, number)
	}
	return []byte(`{"status":"COMPLETED","request_id":"fake-local","finish_reason":"stop","text":"合成回答"}`), nil
}
func (f *fakeAdapter) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

type fixture struct {
	runner             *OfflineRunner
	request            modelgateway.Request
	ref                modelconfiguration.RunReference
	registry           *modelcapability.Registry
	configs            *modelconfiguration.Registry
	primary, secondary *fakeAdapter
	resolve            resolverFunc
}

func makeFixture(t *testing.T) *fixture {
	t.Helper()
	now := time.Now().UTC()
	f := &fixture{}
	f.primary = &fakeAdapter{key: modelcapability.Key{Provider: "fake-primary", Model: "fake-text", Version: "fixture-v1", WireContract: "wire.v1"}}
	f.secondary = &fakeAdapter{key: modelcapability.Key{Provider: "fake-secondary", Model: "fake-text", Version: "fixture-v1", WireContract: "wire.v1"}}
	var records []modelcapability.Record
	for i, a := range []*fakeAdapter{f.primary, f.secondary} {
		records = append(records, modelcapability.Record{Key: a.key, Text: modelcapability.Supported, Capabilities: modelcapability.Capabilities{
			Vision: modelcapability.Unsupported, Tools: modelcapability.Supported, Schema: modelcapability.Supported, Streaming: modelcapability.Unsupported, State: modelcapability.Unsupported, Storage: modelcapability.Unsupported},
			ValidatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Regions: []modelcapability.Region{modelcapability.EU}, Evidence: modelcapability.OfflineContract, QualityRank: 100 - i})
	}
	var err error
	f.registry, err = modelcapability.NewRegistry(records)
	if err != nil {
		t.Fatal(err)
	}
	c := modelconfiguration.Configuration{SchemaVersion: modelconfiguration.SchemaVersion, Version: "local_config.v1", TaskKind: modelgateway.ActivityQuery, PromptVersion: "local_prompt.v1", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", OutputMode: modelgateway.Text, ToolAllowlist: []string{}, PolicyVersion: "local_policy.v1", CapabilitiesRequired: []string{"text"}}
	f.configs, err = modelconfiguration.NewRegistry([]modelconfiguration.PromptDefinition{{Version: c.PromptVersion, Text: "只使用明确提供的合成资料。"}}, []modelconfiguration.PolicyVersionReference{{Version: c.PolicyVersion, ArtifactSHA256: strings.Repeat("b", 64)}}, []modelconfiguration.Configuration{c})
	if err != nil {
		t.Fatal(err)
	}
	id := func(n string) string { return "79000000-0000-4000-8000-00000000000" + n }
	base := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: id("1"), Agent: agentcognitive.AgentReference{AgentID: id("2"), Principal: actorref.PrincipalRef{Type: actorref.Person, ID: id("3")}, Role: agentruntime.PersonalAgent},
		ContextSnapshotRef: id("4"), DataPolicyRef: id("5"), BudgetRef: id("6"), Budget: modelgateway.Budget{MaxOutputTokens: 128}, Messages: []modelgateway.Message{{Role: "user", Content: "合成活动问题，private-input-sentinel"}}, DeadlineAt: now.Add(time.Minute)}
	resolved, _ := f.configs.Resolve(c.Version)
	f.request, f.ref, err = modelconfiguration.PrepareRequest(resolved, base, now)
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(30 * time.Second)
	f.resolve = func(_ context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
		v := agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("a", 64)}
		return modelcapability.OfflineGrant{Agent: r.Agent, RequestDigest: modelcapability.RequestDigest(r, n), RegistryDigest: f.registry.Digest(), SourceVersion: v, CurrentSourceVersion: v, Purpose: "MODEL_CONTEXT_EGRESS", ConsentRevision: 1, CurrentConsentRevision: 1, PolicyRevision: 2, CurrentPolicyRevision: 2, State: "ALLOWED", CheckedAt: at, ExpiresAt: expires,
			Destinations: []modelcapability.Destination{{Key: f.primary.key, Region: modelcapability.EU}, {Key: f.secondary.key, Region: modelcapability.EU}}}, nil
	}
	p := Policy{MaxAttempts: 4, MaxElapsed: 5 * time.Second, AttemptTimeout: time.Second, BaseBackoff: time.Millisecond, MaxBackoff: 100 * time.Millisecond, JitterPermille: 200, SameRouteAttempts: 2, MaxProviderSwitches: 1}
	f.runner, err = NewOfflineRunner(f.registry, []modelcapability.OfflineAdapter{f.primary, f.secondary}, f.configs, resolverFunc(func(ctx context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
		return f.resolve(ctx, r, n, at)
	}), p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func complete(f *fixture) (Outcome, error) {
	return f.runner.Complete(context.Background(), f.request, modelcapability.Requirements{}, f.ref)
}

func TestBoundedRetryReallyUsesGatewayHintAndKeepsRequestFixed(t *testing.T) {
	f := makeFixture(t)
	hint := 15 * time.Millisecond
	f.primary.invoke = func(_ context.Context, n int) ([]byte, error) {
		if n == 1 {
			return nil, modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: false}, hint)
		}
		return []byte(`{"status":"COMPLETED","request_id":"fake-retry","finish_reason":"stop","text":"合成重试回答"}`), nil
	}
	out, err := complete(f)
	if err != nil || out.Result.Status != modelgateway.Completed || f.primary.count() != 2 || f.secondary.count() != 0 || len(out.Attempts) != 2 || out.Attempts[0].ScheduledWait < hint {
		t.Fatal("real retry failed", out, err)
	}
	if f.primary.times[1].Sub(f.primary.times[0]) < hint {
		t.Fatal("retried before Retry-After")
	}
	if !reflect.DeepEqual(f.primary.requests[0], f.primary.requests[1]) {
		t.Fatal("retry changed original prompt/schema/request deadline")
	}
	for _, a := range out.Attempts {
		if a.ConfigurationFingerprint != f.ref.ConfigurationFingerprint {
			t.Fatal("configuration drift")
		}
	}
}

func TestAuthorizedFallbackUsesActualSecondaryDescriptor(t *testing.T) {
	f := makeFixture(t)
	f.runner.policy.SameRouteAttempts = 1
	f.primary.invoke = func(context.Context, int) ([]byte, error) {
		return nil, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: false}
	}
	out, err := complete(f)
	if err != nil || f.primary.count() != 1 || f.secondary.count() != 1 || out.Result.ProviderID != f.secondary.key.Provider || len(out.Attempts) != 2 || out.Attempts[1].Destination != f.secondary.key {
		t.Fatal("fallback did not use genuine secondary route", out, err)
	}
	if !reflect.DeepEqual(f.primary.requests[0], f.secondary.requests[0]) {
		t.Fatal("fallback changed immutable request")
	}
}

func TestRefusalAndUnknownFailuresNeverHopOrRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    string
		err    error
		status modelgateway.Status
	}{
		{"refusal_error", "", modelgateway.ProviderError{Code: "REFUSED", Retryable: true}, modelgateway.Unavailable},
		{"auth", "", modelgateway.ProviderError{Code: "AUTHENTICATION", Retryable: true}, modelgateway.Unavailable},
		{"invalid_request", "", modelgateway.ProviderError{Code: "INVALID_REQUEST", Retryable: true}, modelgateway.Unavailable},
		{"unknown_network_or_tool_outcome", "", errors.New("secret-key-private-input-sentinel unknown effect"), modelgateway.Unavailable},
		{"deadline_error", "", context.DeadlineExceeded, modelgateway.Unavailable},
		{"refusal_response", `{"status":"REFUSED","request_id":"fake-refusal","finish_reason":"refusal"}`, nil, modelgateway.Refused},
		{"truncated", `{"status":"TRUNCATED","request_id":"fake-truncated","finish_reason":"length","text":"partial-private-input-sentinel"}`, nil, modelgateway.Truncated},
		{"unavailable_response", `{"status":"UNAVAILABLE","request_id":"fake-empty","finish_reason":"unavailable"}`, nil, modelgateway.Unavailable},
		{"malformed_schema", `{"status":"COMPLETED","request_id":"fake-invalid","finish_reason":"stop","text":"private-input-sentinel","confirmed":true}`, nil, modelgateway.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := makeFixture(t)
			f.runner.policy.SameRouteAttempts = 1
			f.primary.invoke = func(context.Context, int) ([]byte, error) { return []byte(tc.raw), tc.err }
			out, _ := complete(f)
			if f.primary.count() != 1 || f.secondary.count() != 0 || out.Result.Status != tc.status {
				t.Fatal("terminal failure retried/hopped", out)
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "private-input-sentinel") || strings.Contains(string(b), "secret-key") {
				t.Fatal("private failure leaked")
			}
		})
	}
}

func TestCurrentSourceAndPermissionFailuresAreClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*modelcapability.OfflineGrant)
	}{
		{"revoked", func(g *modelcapability.OfflineGrant) { g.Revoked = true }}, {"deleted", func(g *modelcapability.OfflineGrant) { g.Deleted = true }},
		{"agent_changed", func(g *modelcapability.OfflineGrant) { g.Agent.AgentID = "79000000-0000-4000-8000-000000000009" }},
		{"owner_changed", func(g *modelcapability.OfflineGrant) { g.Agent.Principal.ID = "79000000-0000-4000-8000-000000000009" }},
		{"registry_changed", func(g *modelcapability.OfflineGrant) { g.RegistryDigest = strings.Repeat("f", 64) }},
		{"source_changed", func(g *modelcapability.OfflineGrant) { g.CurrentSourceVersion.Token = strings.Repeat("f", 64) }},
		{"consent_changed", func(g *modelcapability.OfflineGrant) { g.CurrentConsentRevision++ }}, {"policy_changed", func(g *modelcapability.OfflineGrant) { g.CurrentPolicyRevision++ }},
		{"wrong_purpose", func(g *modelcapability.OfflineGrant) { g.Purpose = "MODEL_ANALYSIS" }},
		{"expired", func(g *modelcapability.OfflineGrant) { g.ExpiresAt = g.CheckedAt }}, {"stale_check", func(g *modelcapability.OfflineGrant) { g.CheckedAt = g.CheckedAt.Add(-time.Nanosecond) }},
		{"region_changed", func(g *modelcapability.OfflineGrant) {
			for i := range g.Destinations {
				g.Destinations[i].Region = modelcapability.US
			}
		}},
		{"empty_routes", func(g *modelcapability.OfflineGrant) { g.Destinations = nil }}, {"unknown_state", func(g *modelcapability.OfflineGrant) { g.State = "UNVERIFIED" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := makeFixture(t)
			base := f.resolve
			f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
				g, e := base(c, r, n, at)
				tc.change(&g)
				return g, e
			}
			out, err := complete(f)
			if err == nil || f.primary.count() != 0 || f.secondary.count() != 0 || out.Result.Text != "" {
				t.Fatal("permission error allowed adapter", out, err)
			}
		})
	}
}

func TestOriginalGrantCannotExpandFallbackOrRenewExpiry(t *testing.T) {
	for _, name := range []string{"added_destination", "changed_region", "changed_all_source_versions", "changed_all_policy_versions", "changed_all_consent_versions", "expired_original"} {
		t.Run(name, func(t *testing.T) {
			f := makeFixture(t)
			f.runner.policy.SameRouteAttempts = 1
			f.primary.invoke = func(context.Context, int) ([]byte, error) {
				return nil, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: true}
			}
			base := f.resolve
			calls := 0
			f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
				g, e := base(c, r, n, at)
				calls++
				if name == "added_destination" && calls == 1 {
					g.Destinations = g.Destinations[:1]
				}
				if name == "changed_region" && calls >= 2 {
					g.Destinations[0].Region = modelcapability.US
				}
				if name == "changed_all_source_versions" && calls >= 2 {
					g.SourceVersion.Token = strings.Repeat("f", 64)
					g.CurrentSourceVersion = g.SourceVersion
				}
				if name == "changed_all_policy_versions" && calls >= 2 {
					g.PolicyRevision++
					g.CurrentPolicyRevision++
				}
				if name == "changed_all_consent_versions" && calls >= 2 {
					g.ConsentRevision++
					g.CurrentConsentRevision++
				}
				if name == "expired_original" {
					if calls == 1 {
						g.ExpiresAt = at.Add(5 * time.Millisecond)
					} else {
						g.ExpiresAt = at.Add(time.Second)
					}
				}
				return g, e
			}
			if name == "expired_original" {
				f.primary.invoke = func(ctx context.Context, _ int) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }
			}
			out, err := complete(f)
			if err == nil || f.secondary.count() != 0 || f.primary.count() != 1 || out.Result.Text != "" {
				t.Fatal("original permission expanded or renewed", out, err)
			}
		})
	}
}

func TestGrantRecheckAfterCompletionAndBeforeRetry(t *testing.T) {
	for _, point := range []string{"before_release", "before_retry"} {
		t.Run(point, func(t *testing.T) {
			f := makeFixture(t)
			base := f.resolve
			revoked := false
			f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
				g, e := base(c, r, n, at)
				g.Revoked = revoked
				return g, e
			}
			if point == "before_release" {
				f.primary.invoke = func(context.Context, int) ([]byte, error) {
					revoked = true
					return []byte(`{"status":"COMPLETED","request_id":"fake-late","finish_reason":"stop","text":"private-input-sentinel"}`), nil
				}
			} else {
				f.primary.invoke = func(context.Context, int) ([]byte, error) {
					return nil, modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}
				}
				f.runner.wait = func(context.Context, time.Duration) error { revoked = true; return nil }
			}
			out, err := complete(f)
			if err == nil || f.primary.count() != 1 || f.secondary.count() != 0 || out.Result.Text != "" {
				t.Fatal("revoked response/request released", out, err)
			}
		})
	}
}

func TestAttemptWaitAndDeadlineBudgetsAreActuallyBounded(t *testing.T) {
	for _, name := range []string{"max_one", "max_three", "hint_over_cap", "invalid_hint", "wait_over_deadline", "cooperative_timeout", "late_result", "cancel_wait", "resolver_failure"} {
		t.Run(name, func(t *testing.T) {
			f := makeFixture(t)
			f.runner.policy.MaxProviderSwitches = 0
			f.primary.invoke = func(context.Context, int) ([]byte, error) {
				return nil, modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}
			}
			expect := 1
			ctx := context.Background()
			switch name {
			case "max_one":
				f.runner.policy.MaxAttempts = 1
				f.runner.policy.SameRouteAttempts = 1
			case "max_three":
				f.runner.policy.MaxAttempts = 3
				expect = 3
			case "hint_over_cap":
				f.primary.invoke = func(context.Context, int) ([]byte, error) {
					return nil, modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}, time.Second)
				}
			case "invalid_hint":
				f.primary.invoke = func(context.Context, int) ([]byte, error) {
					return nil, modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}, -1)
				}
			case "wait_over_deadline":
				f.runner.policy.MaxElapsed = 5 * time.Millisecond
				f.runner.policy.AttemptTimeout = 5 * time.Millisecond
				f.primary.invoke = func(context.Context, int) ([]byte, error) {
					return nil, modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}, 50*time.Millisecond)
				}
			case "cooperative_timeout":
				f.runner.policy.AttemptTimeout = 5 * time.Millisecond
				f.primary.invoke = func(c context.Context, _ int) ([]byte, error) { <-c.Done(); return nil, c.Err() }
			case "late_result":
				f.runner.policy.AttemptTimeout = 5 * time.Millisecond
				f.primary.invoke = func(c context.Context, _ int) ([]byte, error) {
					<-c.Done()
					return []byte(`{"status":"COMPLETED","request_id":"fake-expired","finish_reason":"stop","text":"private-input-sentinel"}`), nil
				}
			case "cancel_wait":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				f.runner.wait = func(c context.Context, d time.Duration) error { cancel(); return waitContext(c, d) }
			case "resolver_failure":
				expect = 0
				f.resolve = func(context.Context, modelgateway.Request, modelcapability.Requirements, time.Time) (modelcapability.OfflineGrant, error) {
					return modelcapability.OfflineGrant{}, errors.New("private secret resolver")
				}
			}
			out, err := f.runner.Complete(ctx, f.request, modelcapability.Requirements{}, f.ref)
			if err == nil || f.primary.count() != expect || f.secondary.count() != 0 || out.Result.Text != "" || len(out.Attempts) != expect {
				t.Fatal("budget/timeout unbounded", out, err, f.primary.count())
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("raw resolver error leaked")
			}
		})
	}
}

func TestImmutableConfigurationRefAndUnsupportedNeedsFailBeforeAdapter(t *testing.T) {
	for _, name := range []string{"missing_version", "bad_fingerprint", "prompt_change", "run_change", "agent_change", "tool_change", "output_schema_change", "vision", "streaming", "state", "storage", "nil_context", "cancelled_context"} {
		t.Run(name, func(t *testing.T) {
			f := makeFixture(t)
			r := copyRequest(f.request)
			ref := modelconfiguration.CloneReference(f.ref)
			needs := modelcapability.Requirements{}
			ctx := context.Background()
			switch name {
			case "missing_version":
				ref.ConfigurationVersion = "missing.v1"
			case "bad_fingerprint":
				ref.ConfigurationFingerprint = strings.Repeat("f", 64)
			case "prompt_change":
				r.Messages[0].Content = "替换未经固定的提示"
			case "run_change":
				r.RunID = "79000000-0000-4000-8000-000000000009"
			case "agent_change":
				r.Agent.AgentID = "79000000-0000-4000-8000-000000000009"
			case "tool_change":
				r.ToolAllowlist = []string{"activity.detail"}
			case "output_schema_change":
				r.OutputSchemaVersion = "air.unknown.v1"
			case "vision":
				needs.Vision = true
			case "streaming":
				needs.Streaming = true
			case "state":
				needs.State = true
			case "storage":
				needs.Storage = true
			case "nil_context":
				ctx = nil
			case "cancelled_context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := f.runner.Complete(ctx, r, needs, ref)
			if err == nil || f.primary.count() != 0 || f.secondary.count() != 0 || len(out.Attempts) != 0 {
				t.Fatal("unbound request called provider", out, err)
			}
		})
	}
}

type gate bool

func (g gate) InferenceEnabled(context.Context) bool { return bool(g) }
func TestDefaultServiceCannotUseOfflineGrantsOrEnableProviders(t *testing.T) {
	f := makeFixture(t)
	for _, s := range []*Service{nil, NewService(nil), NewService(gate(false)), NewService(gate(true))} {
		result, err := s.Complete(context.Background(), f.request)
		if !errors.Is(err, modelgateway.ErrUnavailable) || result.Mode != modelgateway.Disabled || result.Text != "" || f.primary.count() != 0 {
			t.Fatal("live gateway enabled")
		}
	}
	for _, name := range []string{"nil_registry", "nil_configs", "nil_resolver", "typed_nil_resolver", "invalid_policy", "live_descriptor"} {
		t.Run(name, func(t *testing.T) {
			registry, configs, resolver, p := f.registry, f.configs, OfflineResolver(f.resolve), f.runner.policy
			adapters := []modelcapability.OfflineAdapter{f.primary}
			switch name {
			case "nil_registry":
				registry = nil
			case "nil_configs":
				configs = nil
			case "nil_resolver":
				resolver = nil
			case "typed_nil_resolver":
				var fn resolverFunc
				resolver = fn
			case "invalid_policy":
				p.MaxAttempts = 0
			case "live_descriptor":
				adapters = []modelcapability.OfflineAdapter{badModeAdapter{}}
			}
			if _, e := NewOfflineRunner(registry, adapters, configs, resolver, p); e == nil {
				t.Fatal("invalid/live constructor accepted")
			}
		})
	}
	g, _ := f.resolve(context.Background(), f.request, modelcapability.Requirements{}, time.Now())
	if _, e := json.Marshal(g); e == nil {
		t.Fatal("offline grant gained client serialization")
	}
}

type badModeAdapter struct{}

func (badModeAdapter) Descriptor() modelgateway.ProviderDescriptor {
	return modelgateway.ProviderDescriptor{ProviderID: "fake-live", ModelID: "fake-model", ModelVersion: "fixture-v1", Mode: "LIVE"}
}
func (badModeAdapter) WireContractVersion() string { return "wire.v1" }
func (badModeAdapter) Complete(context.Context, modelgateway.ProviderRequest) ([]byte, error) {
	return nil, errors.New("must not run")
}

func TestConcurrentLocalInvocationsDoNotShareOrReplenishAttemptCounters(t *testing.T) {
	f := makeFixture(t)
	f.runner.policy.MaxAttempts = 1
	f.runner.policy.SameRouteAttempts = 1
	f.runner.policy.MaxProviderSwitches = 0
	f.primary.invoke = func(context.Context, int) ([]byte, error) {
		return nil, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: true}
	}
	var wg sync.WaitGroup
	errorsCh := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := complete(f)
			if !errors.Is(err, ErrBudget) || len(out.Attempts) != 1 {
				errorsCh <- "unbounded local counter"
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for e := range errorsCh {
		t.Error(e)
	}
	if f.primary.count() != 16 || f.secondary.count() != 0 {
		t.Fatal("local invocation counts drifted")
	}
	// These are intentionally independent invocations, not AIR011 shared money
	// or a persistent root-trace budget. No claim is made about that ledger.
}

func TestDelayedCurrentResolverCannotDispatchExpiredCapabilityOrReleaseExpiredGrant(t *testing.T) {
	for _, point := range []string{"before_dispatch", "before_release"} {
		t.Run(point, func(t *testing.T) {
			f := makeFixture(t)
			base := f.resolve
			checks := 0
			capabilityExpires := time.Now().Add(150 * time.Millisecond)
			if point == "before_dispatch" {
				records := f.registry.Records()
				for i := range records {
					records[i].ExpiresAt = capabilityExpires
				}
				var err error
				f.registry, err = modelcapability.NewRegistry(records)
				if err != nil {
					t.Fatal(err)
				}
				f.runner.registry = f.registry
			}
			f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
				checks++
				g, err := base(c, r, n, at)
				if (point == "before_dispatch" && checks == 1) || (point == "before_release" && checks == 2) {
					delay := 12 * time.Millisecond
					if point == "before_release" {
						g.ExpiresAt = at.Add(4 * time.Millisecond)
					} else {
						delay = time.Until(capabilityExpires) + 5*time.Millisecond
					}
					select {
					case <-c.Done():
						return modelcapability.OfflineGrant{}, c.Err()
					case <-time.After(delay):
					}
				}
				return g, err
			}
			out, err := complete(f)
			want := 0
			if point == "before_release" {
				want = 1
			}
			if err == nil || out.Result.Text != "" || f.primary.count() != want || f.secondary.count() != 0 {
				t.Fatal("expired resolver snapshot dispatched/released", out, err, f.primary.count())
			}
		})
	}
}

func TestDeltaSecondsHeaderUsesRealContextWait(t *testing.T) {
	f := makeFixture(t)
	f.runner.policy.MaxBackoff = 2 * time.Second
	f.runner.policy.AttemptTimeout = 2 * time.Second
	f.primary.invoke = func(_ context.Context, n int) ([]byte, error) {
		if n == 1 {
			return nil, modelgateway.NewRetryAfterHeaderError(modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: false}, "1", time.Now())
		}
		return []byte(`{"status":"COMPLETED","request_id":"fake-after-header","finish_reason":"stop","text":"合成限流后回答"}`), nil
	}
	out, err := complete(f)
	if err != nil || f.primary.count() != 2 || f.secondary.count() != 0 || out.Attempts[0].ScheduledWait < time.Second || f.primary.times[1].Sub(f.primary.times[0]) < time.Second {
		t.Fatal("429 header was not observed by real wait", out, err)
	}
}

func replaceMode(t *testing.T, f *fixture, mode modelgateway.OutputMode, kind modelgateway.TaskKind) {
	t.Helper()
	config := modelconfiguration.Configuration{SchemaVersion: modelconfiguration.SchemaVersion, Version: "local_config.v2", TaskKind: kind, PromptVersion: "local_prompt.v2", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", OutputMode: mode, ToolAllowlist: []string{}, PolicyVersion: "local_policy.v2", CapabilitiesRequired: []string{"text", "structured_output_validatable"}}
	if mode == modelgateway.ToolProposals {
		config.ToolAllowlist = []string{"activity.search", "activity.detail"}
		config.CapabilitiesRequired = append(config.CapabilitiesRequired, "tools")
	}
	if kind == modelgateway.MemoryCandidateExtraction {
		config.OutputSchemaVersion = "air.candidate_proposal.v1"
	}
	var err error
	f.configs, err = modelconfiguration.NewRegistry([]modelconfiguration.PromptDefinition{{Version: config.PromptVersion, Text: "只输出可核验的合成提案。"}}, []modelconfiguration.PolicyVersionReference{{Version: config.PolicyVersion, ArtifactSHA256: strings.Repeat("c", 64)}}, []modelconfiguration.Configuration{config})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := f.configs.Resolve(config.Version)
	if err != nil {
		t.Fatal(err)
	}
	r := copyRequest(f.request)
	r.Messages = r.Messages[1:]
	f.request, f.ref, err = modelconfiguration.PrepareRequest(resolved, r, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.runner.configurations = f.configs
}

func TestRetryPreservesStructuredAndToolProposalContracts(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode modelgateway.OutputMode
		kind modelgateway.TaskKind
		raw  string
		bad  bool
	}{
		{"answer", modelgateway.Structured, modelgateway.ActivityQuery, `{"status":"COMPLETED","request_id":"fake-answer","finish_reason":"stop","structured":{"answer":"合成活动提案","entity_refs":[]}}`, false},
		{"tools_only_proposals", modelgateway.ToolProposals, modelgateway.ActivityQuery, `{"status":"COMPLETED","request_id":"fake-tools","finish_reason":"stop","tool_proposals":[{"tool":"activity.search","arguments":{"query":"合成羽毛球","city_id":"aberdeen-gb"},"reason_summary":"合成只读提案"}]}`, false},
		{"unknown_tool_outcome", modelgateway.ToolProposals, modelgateway.ActivityQuery, "", true},
		{"invalid_proposal_schema", modelgateway.ToolProposals, modelgateway.ActivityQuery, `{"status":"COMPLETED","request_id":"fake-bad-tools","finish_reason":"stop","tool_proposals":[{"tool":"activity.delete","arguments":{"activity_id":"79000000-0000-4000-8000-000000000009"},"reason_summary":"invalid"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := makeFixture(t)
			replaceMode(t, f, tc.mode, tc.kind)
			f.primary.invoke = func(_ context.Context, n int) ([]byte, error) {
				if tc.name == "unknown_tool_outcome" {
					return nil, errors.New("unknown tool side effect, private-input-sentinel")
				}
				if n == 1 && !tc.bad {
					return nil, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: true}
				}
				return []byte(tc.raw), nil
			}
			out, err := complete(f)
			if tc.bad {
				if err == nil || f.primary.count() != 1 || f.secondary.count() != 0 || len(out.Result.ToolProposals) != 0 {
					t.Fatal("bad or unknown tool outcome retried/released", out, err)
				}
			} else {
				if err != nil || f.primary.count() != 2 || out.Result.Status != modelgateway.Completed || !reflect.DeepEqual(f.primary.requests[0], f.primary.requests[1]) {
					t.Fatal("proposal retry broke closed contract", out, err)
				}
				if tc.mode == modelgateway.ToolProposals && len(out.Result.ToolProposals) != 1 {
					t.Fatal("proposal missing")
				}
			}
		})
	}
}

func TestShorterRequestOrContextDeadlineDoesNotReceiveFreshTime(t *testing.T) {
	for _, which := range []string{"request", "context"} {
		t.Run(which, func(t *testing.T) {
			f := makeFixture(t)
			ctx := context.Background()
			if which == "request" {
				f.request.DeadlineAt = time.Now().Add(6 * time.Millisecond)
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 6*time.Millisecond)
				defer cancel()
			}
			base := f.resolve
			f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
				g, e := base(c, r, n, at)
				if g.ExpiresAt.After(r.DeadlineAt) {
					g.ExpiresAt = r.DeadlineAt
				}
				return g, e
			}
			f.primary.invoke = func(c context.Context, _ int) ([]byte, error) {
				<-c.Done()
				return []byte(`{"status":"COMPLETED","request_id":"fake-context-expired","finish_reason":"stop","text":"private-input-sentinel"}`), nil
			}
			out, err := f.runner.Complete(ctx, f.request, modelcapability.Requirements{}, f.ref)
			if err == nil || f.primary.count() > 1 || f.secondary.count() != 0 || out.Result.Text != "" {
				t.Fatal("request/context deadline renewed", out, err)
			}
		})
	}
}

func TestRestrictGrantCopiesAndOnlyShrinksOriginalPermissions(t *testing.T) {
	f := makeFixture(t)
	at := time.Now()
	original, _ := f.resolve(context.Background(), f.request, modelcapability.Requirements{}, at)
	original.Destinations = original.Destinations[:1]
	original.Destinations[0].AllowState = false
	original.Destinations[0].AllowStorage = false
	current, _ := f.resolve(context.Background(), f.request, modelcapability.Requirements{}, at)
	current.Destinations[0].AllowState = true
	current.Destinations[0].AllowStorage = true
	current.ExpiresAt = original.ExpiresAt.Add(time.Second)
	narrowed, err := restrict(current, original, nil)
	if err != nil || len(narrowed.Destinations) != 1 || narrowed.Destinations[0].AllowState || narrowed.Destinations[0].AllowStorage || !narrowed.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatal("permissions grew", narrowed, err)
	}
	narrowed.Destinations[0].Region = modelcapability.US
	if current.Destinations[0].Region != modelcapability.EU || original.Destinations[0].Region != modelcapability.EU {
		t.Fatal("restriction leaked mutable destinations")
	}
	if _, err = restrict(current, original, map[modelcapability.Key]bool{f.primary.key: true}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("excluded primary was replaced by unapproved secondary")
	}
}

func TestResolverCannotMutatePinnedRequestThroughItsCopy(t *testing.T) {
	f := makeFixture(t)
	original := copyRequest(f.request)
	base := f.resolve
	f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
		g, e := base(c, r, n, at)
		r.Messages[0].Content = "mutated system"
		r.CapabilitiesRequired[0] = "tools"
		return g, e
	}
	out, err := complete(f)
	if err != nil || out.Result.Status != modelgateway.Completed || !reflect.DeepEqual(f.request, original) || f.primary.requests[0].Messages[0].Content != original.Messages[0].Content {
		t.Fatal("resolver mutation altered pinned request", out, err)
	}
}

func TestSyntheticPermissionNarrowingCannotBypassRouteSwitchBudget(t *testing.T) {
	f := makeFixture(t)
	f.runner.policy.MaxProviderSwitches = 0
	f.primary.invoke = func(context.Context, int) ([]byte, error) {
		return nil, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: true}
	}
	base := f.resolve
	checks := 0
	f.resolve = func(c context.Context, r modelgateway.Request, n modelcapability.Requirements, at time.Time) (modelcapability.OfflineGrant, error) {
		g, e := base(c, r, n, at)
		checks++
		if checks >= 3 {
			g.Destinations = g.Destinations[1:]
		}
		return g, e
	}
	out, err := complete(f)
	if !errors.Is(err, ErrBudget) || len(out.Attempts) != 1 || f.primary.count() != 1 || f.secondary.count() != 0 {
		t.Fatal("fresh grant bypassed switch budget", out, err)
	}
}
