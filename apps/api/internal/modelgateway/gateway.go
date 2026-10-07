package modelgateway

import (
	"context"
	"reflect"
	"time"
)

type ModelGateway interface {
	Complete(context.Context, Request) (Result, error)
}

// LiveGate is a trusted server rollout brake only. It is intentionally narrow;
// an AGE066 controller adapter can implement it without changing that package.
// Passing this gate does not manufacture source/purpose/egress/budget approval.
type LiveGate interface{ InferenceEnabled(context.Context) bool }

// The default constructor has no provider registration. The separate trusted
// constructor requires a native dispatch port; construction is never a grant.
type Gateway struct {
	gate LiveGate
	now  func() time.Time
	live *nativeLiveBoundary
}

var _ ModelGateway = (*Gateway)(nil)

func NewGateway(gate LiveGate) *Gateway {
	if gate != nil {
		v := reflect.ValueOf(gate)
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if v.IsNil() {
				gate = nil
			}
		}
	}
	return &Gateway{gate: gate, now: time.Now}
}

func (g *Gateway) Complete(ctx context.Context, r Request) (Result, error) {
	if g == nil || g.now == nil {
		return emptyResult(Request{}, Disabled, Unavailable, "gateway_unavailable"), ErrUnavailable
	}
	if ctx == nil {
		return emptyResult(Request{}, Disabled, Invalid, "invalid_context"), ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return emptyResult(Request{}, Disabled, Unavailable, "cancelled"), err
	}
	if ValidateRequest(r, g.now()) != nil {
		return emptyResult(Request{}, Disabled, Invalid, "invalid_request"), ErrInvalid
	}
	if g.gate == nil || !g.gate.InferenceEnabled(ctx) {
		return emptyResult(r, Disabled, Unavailable, "inference_disabled"), ErrUnavailable
	}
	if g.live != nil {
		return g.completeNativeLive(ctx, r)
	}
	// There is deliberately no conditional call to a provider here, even if
	// a fixture gate returns true. Flags do not prove any business authority.
	return emptyResult(r, Disabled, Unavailable, "current_authority_egress_budget_provider_unavailable"), ErrUnavailable
}

// OfflineHarness is a separate synthetic contract runner, never a live grant
// or a fallback from Gateway. Only trusted developers constructing a local
// fake adapter may use it; there is no HTTP/main/domain call site.
type OfflineHarness struct {
	adapter    ProviderAdapter
	descriptor ProviderDescriptor
	now        func() time.Time
}

var _ ModelGateway = (*OfflineHarness)(nil)

func NewOfflineHarness(adapter ProviderAdapter) (*OfflineHarness, error) {
	if !adapterPresent(adapter) {
		return nil, ErrUnavailable
	}
	descriptor := adapter.Descriptor()
	if !validDescriptor(descriptor) {
		return nil, ErrUnavailable
	}
	return &OfflineHarness{adapter: adapter, descriptor: descriptor, now: time.Now}, nil
}

func (h *OfflineHarness) Complete(ctx context.Context, r Request) (Result, error) {
	if h == nil || h.now == nil || !adapterPresent(h.adapter) {
		return emptyResult(Request{}, OfflineContract, Unavailable, "offline_adapter_unavailable"), ErrUnavailable
	}
	if ctx == nil {
		return emptyResult(Request{}, OfflineContract, Invalid, "invalid_context"), ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return emptyResult(Request{}, OfflineContract, Unavailable, "cancelled"), err
	}
	if ValidateRequest(r, h.now()) != nil {
		return emptyResult(Request{}, OfflineContract, Invalid, "invalid_request"), ErrInvalid
	}
	// Capture local immutable copies before handing a request to an adapter.
	allowlist := append([]string{}, r.ToolAllowlist...)
	r.ToolAllowlist = allowlist
	if h.adapter.Descriptor() != h.descriptor || !validDescriptor(h.descriptor) {
		return emptyResult(r, OfflineContract, Unavailable, "adapter_changed"), ErrUnavailable
	}
	callContext, cancel := context.WithDeadline(ctx, r.DeadlineAt)
	defer cancel()
	if callContext.Err() != nil {
		return emptyResult(r, OfflineContract, Unavailable, "deadline_exceeded"), ErrDeadline
	}
	raw, err := h.adapter.Complete(callContext, providerRequest(r))
	if cancelErr := ctx.Err(); cancelErr != nil {
		return emptyResult(r, OfflineContract, Unavailable, "cancelled"), cancelErr
	}
	if callContext.Err() != nil || !r.DeadlineAt.After(h.now()) {
		return emptyResult(r, OfflineContract, Unavailable, "deadline_exceeded"), ErrDeadline
	}
	if h.adapter.Descriptor() != h.descriptor {
		return emptyResult(r, OfflineContract, Unavailable, "adapter_changed"), ErrUnavailable
	}
	if err != nil {
		providerErr := normalizedProviderError(err)
		return emptyResult(r, OfflineContract, Unavailable, "provider_"+providerErr.Code), normalizedProviderFailure(err, providerErr)
	}
	return normalizeResponse(r, h.descriptor, raw)
}
