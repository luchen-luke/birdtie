package modelgateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"time"
)

type NativeVisualDispatchPort interface {
	CheckCurrent(context.Context, PreparedVisualWire) error
	ReleaseOnce(context.Context, PreparedVisualWire, func(context.Context) (VisualResult, error)) (VisualResult, error)
	SettleUnknown(context.Context, PreparedVisualWire) error
}
type NativeVisualGateway struct {
	gate    LiveGate
	adapter *TencentVisionAdapter
	native  NativeVisualDispatchPort
}

func NewNativeVisualGateway(gate LiveGate, adapter *TencentVisionAdapter, native NativeVisualDispatchPort) (*NativeVisualGateway, error) {
	if !visualDependencyPresent(gate) || adapter == nil || !adapter.config.valid() || !visualDependencyPresent(native) {
		return nil, ErrUnavailable
	}
	return &NativeVisualGateway{gate, adapter, native}, nil
}
func visualDependencyPresent(value any) bool {
	if value == nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return !v.IsNil()
	}
	return true
}
func (g *NativeVisualGateway) Complete(ctx context.Context, p PreparedVisualWire) (VisualResult, error) {
	unknown := VisualResult{Status: Unavailable, Usage: Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}}
	if g == nil || g.gate == nil || g.adapter == nil || g.native == nil || ctx == nil || ctx.Err() != nil || !p.valid(time.Now()) || !g.gate.InferenceEnabled(ctx) {
		return unknown, ErrUnavailable
	}
	ctx, cancel := context.WithDeadline(ctx, p.DeadlineAt())
	defer cancel()
	var closed atomic.Bool
	check := func(c context.Context) error {
		if closed.Load() || c == nil || c.Err() != nil || ctx.Err() != nil || !g.gate.InferenceEnabled(c) || !p.DeadlineAt().After(g.adapter.now()) {
			return ErrUnavailable
		}
		return g.native.CheckCurrent(c, p)
	}
	if e := check(ctx); e != nil {
		return unknown, e
	}
	var calls atomic.Int32
	var attempted atomic.Bool
	var repeated atomic.Bool
	type completedCall struct {
		result VisualResult
		err    error
	}
	done := make(chan completedCall, 1)
	result, e := g.native.ReleaseOnce(ctx, p, func(c context.Context) (VisualResult, error) {
		if !calls.CompareAndSwap(0, 1) {
			repeated.Store(true)
			return unknown, ErrUnavailable
		}
		if e := check(c); e != nil {
			return unknown, e
		}
		attempted.Store(true)
		actual, actualError := g.adapter.complete(c, p, check)
		done <- completedCall{actual, actualError}
		return actual, actualError
	})
	closed.Store(true)
	var completed completedCall
	returned := false
	select {
	case completed = <-done:
		returned = true
	default:
	}
	settle := func() {
		settlectx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		_ = g.native.SettleUnknown(settlectx, p)
	}
	if e != nil || calls.Load() != 1 || repeated.Load() || !returned || completed.err != nil || !attempted.Load() {
		if attempted.Load() {
			settle()
		}
		if e != nil {
			return unknown, e
		}
		if completed.err != nil {
			return unknown, completed.err
		}
		return unknown, ErrUnavailable
	}
	encoded, _ := json.Marshal(result)
	expected, _ := json.Marshal(completed.result)
	if sha256.Sum256(encoded) != sha256.Sum256(expected) || !g.gate.InferenceEnabled(ctx) || ctx.Err() != nil || !(result.Status == Completed || result.Status == Refused) {
		settle()
		return unknown, ErrUnavailable
	}
	// The native port performs the final authoritative source/session and commit
	// checks; a completed HTTP response cannot manufacture that receipt.
	return result, nil
}
