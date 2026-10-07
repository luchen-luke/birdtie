package modelcapability

import (
	"context"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// Service has no approved provider/current source-egress resolver. The live
// gateway is always Disabled/Unavailable even when a fixture brake is ON.
type Service struct{ gateway *modelgateway.Gateway }

func NewService(gate modelgateway.LiveGate) *Service { return &Service{modelgateway.NewGateway(gate)} }
func (s *Service) Complete(ctx context.Context, r modelgateway.Request) (modelgateway.Result, error) {
	if s == nil || s.gateway == nil {
		return modelgateway.NewGateway(nil).Complete(ctx, r)
	}
	return s.gateway.Complete(ctx, r)
}

// OfflineAdapter explicitly describes a local fake's exact wire contract.
// The 007 descriptor already requires Mode=OFFLINE_CONTRACT; this interface
// cannot be injected into Service or cause its disabled live gateway to route.
type OfflineAdapter interface {
	modelgateway.ProviderAdapter
	WireContractVersion() string
}
type OfflineRunner struct {
	registry *Registry
	adapters map[Key]OfflineAdapter
	now      func() time.Time
}

func adapterPresent(a OfflineAdapter) bool {
	if a == nil {
		return false
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan:
		return !v.IsNil()
	}
	return true
}
func adapterKey(a OfflineAdapter) Key {
	d := a.Descriptor()
	return Key{d.ProviderID, d.ModelID, d.ModelVersion, a.WireContractVersion()}
}
func NewOfflineRunner(registry *Registry, adapters []OfflineAdapter) (*OfflineRunner, error) {
	return NewOfflineRunnerWithClock(registry, adapters, time.Now)
}

// NewOfflineRunnerWithClock makes exact synthetic checks callable outside this
// package. The supplied local contract clock is not actual source authority and
// cannot be supplied to Service. Callers must still use current 007 deadlines.
func NewOfflineRunnerWithClock(registry *Registry, adapters []OfflineAdapter, clock func() time.Time) (*OfflineRunner, error) {
	if registry == nil || registry.digest == "" || len(adapters) > MaxRecords || clock == nil {
		return nil, ErrInvalid
	}
	r := &OfflineRunner{registry: registry, adapters: map[Key]OfflineAdapter{}, now: clock}
	for _, a := range adapters {
		if !adapterPresent(a) || a.Descriptor().Mode != modelgateway.OfflineContract {
			return nil, ErrUnavailable
		}
		key := adapterKey(a)
		if !validKey(key) || r.adapters[key] != nil {
			return nil, ErrInvalid
		}
		found := false
		for _, record := range registry.records {
			if record.Key == key && record.Evidence == OfflineContract {
				found = true
			}
		}
		if !found {
			return nil, ErrUnsupported
		}
		r.adapters[key] = a
	}
	return r, nil
}

func (runner *OfflineRunner) Complete(ctx context.Context, r modelgateway.Request, needs Requirements, g OfflineGrant) (modelgateway.Result, error) {
	if runner == nil || runner.now == nil {
		return modelgateway.Result{}, ErrUnavailable
	}
	if ctx == nil {
		return modelgateway.Result{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return modelgateway.Result{}, err
	}
	if err := validateGrant(runner.registry, r, needs, g, runner.now()); err != nil {
		return modelgateway.Result{}, err
	}
	// 007 has no image/state/storage/streaming input or consumer. Never strip a
	// requirement and quietly send the remainder to a text-only fake adapter.
	if needs.Vision || needs.Streaming || needs.State || needs.Storage {
		return modelgateway.Result{}, ErrUnsupported
	}
	plan, err := SelectOffline(runner.registry, r, needs, g, runner.now())
	if err != nil {
		return modelgateway.Result{}, err
	}
	a := runner.adapters[plan.Key]
	if !adapterPresent(a) || adapterKey(a) != plan.Key || a.Descriptor().Mode != modelgateway.OfflineContract {
		return modelgateway.Result{}, ErrUnsupported
	}
	harness, err := modelgateway.NewOfflineHarness(a)
	if err != nil {
		return modelgateway.Result{}, ErrUnavailable
	}
	result, err := harness.Complete(ctx, r)
	if err != nil {
		return result, err
	}
	// Recheck wall-clock authorization and exact adapter contract before release;
	// this synthetic boundary is not an atomic live dispatch/consent resolver.
	now := runner.now()
	stillEligible := false
	for _, record := range runner.registry.records {
		if eligible(record, r, needs, Destination{Key: plan.Key, Region: plan.Region}, now) {
			stillEligible = true
		}
	}
	if g.Revoked || !validTime(now) || !g.ExpiresAt.After(now) || !stillEligible || adapterKey(a) != plan.Key || a.Descriptor().Mode != modelgateway.OfflineContract {
		return modelgateway.Result{}, ErrDenied
	}
	return result, nil
}
