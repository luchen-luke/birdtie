package modelgateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// NativeLiveDispatchPort is a trusted server dependency, not request input.
// Its native implementation must re-read current actor/task/source/egress and
// the original four budget scopes for these exact bytes and their fixed input
// upper bound. ReleaseOnce may invoke the callback synchronously only after
// the original native release commit; uncertain commits must never invoke it.
// SettleUnknown retains the entire original hold, including provider failures,
// cancellation, revocation and response loss. No refund or recovery dispatch.
// This package supplies no main/HTTP registration. The trusted port belongs
// to the original native store; constructing this gateway never activates it.
type NativeLiveDispatchPort interface {
	CheckCurrent(context.Context, Request, PreparedTencentWire) error
	ReleaseOnce(context.Context, Request, PreparedTencentWire, func(context.Context) ([]byte, error)) ([]byte, error)
	SettleUnknown(context.Context, Request, PreparedTencentWire) error
}

type nativeLiveBoundary struct {
	port    NativeLiveDispatchPort
	adapter *TencentTokenHubAdapter
}

// NewNativeLiveGateway retains the same Gateway type. A configured adapter,
// port, rollout flag or Request reference is not an approval. Default
// NewGateway remains unavailable, and OfflineHarness still rejects LIVE.
func NewNativeLiveGateway(gate LiveGate, adapter *TencentTokenHubAdapter, port NativeLiveDispatchPort) (*Gateway, error) {
	g := NewGateway(gate)
	if g.gate == nil || adapter == nil || adapter.client == nil || adapter.now == nil || !adapter.config.valid() || !nativeLivePortPresent(port) {
		return nil, ErrUnavailable
	}
	profile, ok := tencentProfileForModel(adapter.config.model)
	if !ok || !profile.nativeReady() || adapter.Descriptor() != profile.descriptor() {
		return nil, ErrUnavailable
	}
	g.live = &nativeLiveBoundary{port: port, adapter: adapter}
	return g, nil
}

func nativeLivePortPresent(p NativeLiveDispatchPort) bool {
	if p == nil {
		return false
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !v.IsNil()
	}
	return true
}
func tencentLiveDescriptor() ProviderDescriptor {
	profile, _ := tencentProfileForModel(TencentTokenHubModel)
	return profile.descriptor()
}
func cloneLiveRequest(r Request) Request {
	r.Messages = append([]Message(nil), r.Messages...)
	r.ToolAllowlist = append([]string(nil), r.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string(nil), r.CapabilitiesRequired...)
	return r
}

func (g *Gateway) currentLive(ctx context.Context, r Request, p PreparedTencentWire) error {
	if ctx == nil || ctx.Err() != nil || !r.DeadlineAt.After(g.now()) || !p.valid(g.now()) || g.live.adapter.Descriptor() != p.Descriptor() {
		return ErrUnavailable
	}
	if !g.gate.InferenceEnabled(ctx) || ctx.Err() != nil || !r.DeadlineAt.After(g.now()) {
		return ErrUnavailable
	}
	if err := g.live.port.CheckCurrent(ctx, cloneLiveRequest(r), p); err != nil {
		return ErrUnavailable // Never expose a native error's source or payload.
	}
	if ctx.Err() != nil || !r.DeadlineAt.After(g.now()) || !g.gate.InferenceEnabled(ctx) || ctx.Err() != nil || !r.DeadlineAt.After(g.now()) {
		return ErrUnavailable
	}
	return nil
}

type liveWireTransport struct {
	prepared PreparedTencentWire
	next     http.RoundTripper
	attempts *atomic.Int32
	wires    *atomic.Int32
	before   func() error
}

// Transport handoff may precede DNS/TLS or a delayed body writer. Keep exact
// approved bytes immutable and recheck current native authority for every Read,
// including a writer continuing after an early response. No WriterTo bypass is
// implemented. Close retires future reads without mutating shared byte storage.
type nativeLiveRequestBody struct {
	mu     sync.Mutex
	reader *strings.Reader
	ctx    context.Context
	before func() error
	closed atomic.Bool
}

func (b *nativeLiveRequestBody) Read(p []byte) (int, error) {
	if b == nil {
		return 0, modelFailure("MODEL_CURRENT", "NATIVE_CURRENT", 0, ErrUnavailable)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed.Load() || b.ctx == nil || b.ctx.Err() != nil || b.before == nil || b.reader == nil {
		return 0, modelFailure("MODEL_CURRENT", "NATIVE_CURRENT", 0, ErrUnavailable)
	}
	if e := b.before(); e != nil || b.closed.Load() || b.ctx.Err() != nil {
		return 0, modelFailure("MODEL_CURRENT", "NATIVE_CURRENT", 0, ErrUnavailable)
	}
	return b.reader.Read(p)
}
func (b *nativeLiveRequestBody) Close() error {
	if b != nil {
		b.closed.Store(true)
	}
	return nil
}

func (t liveWireTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.Body == nil || req.URL == nil || req.URL.String() != tencentChatEndpoint || req.Method != http.MethodPost || req.GetBody != nil || !req.Close || t.next == nil || t.attempts == nil || t.wires == nil || t.before == nil || !t.attempts.CompareAndSwap(0, 1) {
		return nil, modelFailure("MODEL_WIRE", "NATIVE_WIRE", 0, ErrLivePreparation)
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, MaxRequestBytes+1))
	_ = req.Body.Close()
	if err != nil || !bytes.Equal(raw, []byte(t.prepared.wire)) || tencentWireDigest(raw) != t.prepared.digest {
		clear(raw)
		return nil, modelFailure("MODEL_WIRE", "NATIVE_WIRE", 0, ErrLivePreparation)
	}
	// The body is the exact copy whose digest the native port approved. No
	// automatic GetBody or retry is installed when restoring the read body.
	// net/http may finish writing the request body after RoundTrip returns an
	// early response. Keep its body immutable; clearing a shared byte reader at
	// return could change approved bytes or race that writer.
	req.Body = &nativeLiveRequestBody{reader: strings.NewReader(t.prepared.wire), ctx: req.Context(), before: t.before}
	clear(raw)
	if err := t.before(); err != nil || req.Context().Err() != nil {
		return nil, modelFailure("MODEL_CURRENT", "NATIVE_CURRENT", 0, ErrUnavailable)
	}
	t.wires.Add(1)
	return t.next.RoundTrip(req)
}

func (g *Gateway) completeNativeLive(ctx context.Context, original Request) (Result, error) {
	r := cloneLiveRequest(original)
	fail := func(reason string, err error) (Result, error) {
		return emptyResult(r, Live, Unavailable, reason), WithLiveFailureReason(err, reason)
	}
	p, err := g.live.adapter.Prepare(providerRequest(r))
	if err != nil {
		return fail("live_preparation_unavailable", ErrUnavailable)
	}
	callctx, cancel := context.WithDeadline(ctx, r.DeadlineAt)
	defer cancel()
	if err := g.currentLive(callctx, r, p); err != nil {
		return fail("live_current_authority_unavailable", err)
	}
	var invokes, attempts, wires atomic.Int32
	var callbackInvalid atomic.Bool
	var releaseOpen atomic.Bool
	releaseOpen.Store(true)
	var stateMu sync.Mutex
	var captured []byte
	var callErr error
	finished := false
	returned, releaseErr := g.live.port.ReleaseOnce(callctx, cloneLiveRequest(r), p, func(releasedCtx context.Context) ([]byte, error) {
		if !invokes.CompareAndSwap(0, 1) || !releaseOpen.Load() || releasedCtx == nil || releasedCtx.Err() != nil {
			callbackInvalid.Store(true)
			return nil, ErrUnavailable
		}
		until := r.DeadlineAt
		if nativeUntil, ok := releasedCtx.Deadline(); ok && nativeUntil.Before(until) {
			until = nativeUntil
		}
		invokeCtx, invokeCancel := context.WithDeadline(callctx, until)
		defer invokeCancel()
		stopNativeCancel := context.AfterFunc(releasedCtx, invokeCancel)
		defer stopNativeCancel()
		var raw []byte
		var invocationErr error
		if e := g.currentLive(invokeCtx, r, p); e != nil {
			invocationErr = e
		} else {
			adapter := *g.live.adapter
			client := *adapter.client
			client.Transport = liveWireTransport{prepared: p, next: adapter.client.Transport, attempts: &attempts, wires: &wires, before: func() error {
				if !releaseOpen.Load() || callbackInvalid.Load() {
					return ErrUnavailable
				}
				return g.currentLive(invokeCtx, r, p)
			}}
			adapter.client = &client
			raw, invocationErr = adapter.Complete(invokeCtx, p.request)
			if e := g.currentLive(invokeCtx, r, p); e != nil {
				clear(raw)
				raw, invocationErr = nil, e
			}
		}
		stateMu.Lock()
		captured, callErr, finished = append([]byte(nil), raw...), invocationErr, true
		stateMu.Unlock()
		return raw, invocationErr
	})
	releaseOpen.Store(false)
	// A trusted native implementation must finish synchronously. Snapshot
	// under a lock so a defective asynchronous port cannot race output release.
	stateMu.Lock()
	raw, providerErr, done := append([]byte(nil), captured...), callErr, finished
	stateMu.Unlock()
	defer clear(raw)
	postErr := g.currentLive(callctx, r, p)
	// Settlement is cleanup of the existing native hold, not fresh dispatch or
	// authority. It remains bounded and must run after caller cancellation too.
	settleCtx, settleCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	settleErr := g.live.port.SettleUnknown(settleCtx, cloneLiveRequest(r), p)
	settleCancel()
	releaseFailed := releaseErr != nil && (providerErr == nil || !errors.Is(releaseErr, providerErr))
	if releaseFailed || settleErr != nil || invokes.Load() != 1 || callbackInvalid.Load() || !done || wires.Load() > 1 || !bytes.Equal(returned, raw) {
		return fail("live_release_or_settlement_unknown", ErrUnavailable)
	}
	if postErr != nil || g.currentLive(callctx, r, p) != nil {
		return fail("live_current_authority_unavailable", ErrUnavailable)
	}
	if providerErr != nil {
		classified := normalizedProviderError(providerErr)
		// Preserve the safe adapter diagnostic and original errors.Is/As cause.
		// Normalized ProviderError and RetryAfter retain their original contracts.
		return fail("provider_"+classified.Code, errors.Join(normalizedProviderFailure(providerErr, classified), providerErr))
	}
	if wires.Load() != 1 {
		return fail("live_wire_unavailable", ErrUnavailable)
	}
	result, err := normalizeLiveTencentTextAt(r, raw, p, g.now())
	if err != nil {
		return emptyResult(r, Live, Invalid, "invalid_live_text_response"), WithLiveFailureReason(err, "invalid_live_text_response")
	}
	if g.currentLive(callctx, r, p) != nil {
		return fail("live_current_authority_unavailable", ErrUnavailable)
	}
	return result, nil
}

// The original offline normalizer remains untouched. This LIVE path admits
// only the selected text envelope; no tools, entities, Memory or structured
// answers, and no partial output from a refused/truncated response.
func normalizeLiveTencentText(r Request, raw []byte, p PreparedTencentWire) (Result, error) {
	return normalizeLiveTencentTextAt(r, raw, p, time.Now())
}

func normalizeLiveTencentTextAt(r Request, raw []byte, p PreparedTencentWire, now time.Time) (Result, error) {
	invalid := emptyResult(r, Live, Invalid, "invalid_live_text_response")
	if !p.Matches(providerRequest(r), now) || len(raw) == 0 || len(raw) > MaxResultBytes || r.OutputMode != Text || r.TaskKind != ActivityQuery || len(r.ToolAllowlist) != 0 || r.Budget.MaxOutputTokens != p.MaxOutputTokens() {
		return invalid, ErrAdapter
	}
	obj, err := strictObject(raw, []string{"status", "request_id", "finish_reason"}, []string{"text", "usage"})
	if err != nil {
		return invalid, ErrAdapter
	}
	statusText, e1 := stringValue(obj["status"], 40)
	requestID, e2 := stringValue(obj["request_id"], 80)
	finish, e3 := stringValue(obj["finish_reason"], 40)
	if e1 != nil || e2 != nil || e3 != nil || !safeRequestID.MatchString(requestID) {
		return invalid, ErrAdapter
	}
	usage, err := parseUsage(obj["usage"], p.MaxOutputTokens())
	if err != nil || (usage.InputTokens != nil && *usage.InputTokens > p.InputTokenBound()) {
		return invalid, ErrAdapter
	}
	result := emptyResult(r, Live, Status(statusText), "native_live_text_proposal_only")
	switch result.Status {
	case Completed:
		if finish != "stop" {
			return invalid, ErrAdapter
		}
		result.Text, err = stringValue(obj["text"], 8192)
		if err != nil {
			return invalid, ErrAdapter
		}
	case Refused:
		if finish != "refusal" || obj["text"] != nil {
			return invalid, ErrAdapter
		}
	case Truncated:
		if finish != "length" || obj["text"] != nil {
			return invalid, ErrAdapter
		}
	case Unavailable:
		if finish != "unavailable" || obj["text"] != nil {
			return invalid, ErrAdapter
		}
	default:
		return invalid, ErrAdapter
	}
	descriptor := p.Descriptor()
	result.Usage, result.ProviderID, result.ProviderModelVersion = usage, descriptor.ProviderID, descriptor.ModelID+"@"+descriptor.ModelVersion
	result.ProviderRequestID, result.FinishReason = requestID, finish
	return result, nil
}
