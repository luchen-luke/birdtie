package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// These bridges use the original 062 native reservation. Construction checks
// an existing approved operation; it does not create a preview, consent, budget
// account, Task or provider registration. They have no HTTP/main call site.
// Query-only egress cannot be used to export web snippets or native entities.
type nativeLiveDispatch struct {
	store      *Store
	access     agentevent.Access
	operation  string
	controller *agentfeature.Controller
	ticket     agentfeature.Ticket
	projector  LiveWireProjector
	gate       modelgateway.LiveGate
	used       atomic.Bool
}

func (*nativeLiveDispatch) MarshalJSON() ([]byte, error) { return nil, modelegressbudget.ErrServerOnly }
func (*nativeLiveDispatch) UnmarshalJSON([]byte) error   { return modelegressbudget.ErrServerOnly }

func nativeLiveGatePresent(gate modelgateway.LiveGate) bool {
	if gate == nil {
		return false
	}
	v := reflect.ValueOf(gate)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !v.IsNil()
	}
	return true
}

func (s *Store) newNativeLiveDispatch(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, projector LiveWireProjector, gate modelgateway.LiveGate) (*nativeLiveDispatch, PreparedLiveEgress, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !egressUUID(operation) || c == nil || !nativeLiveGatePresent(gate) || !gate.InferenceEnabled(ctx) {
		return nil, PreparedLiveEgress{}, modelegressbudget.ErrUnavailable
	}
	ticket, err := egressTicket(c)
	if err != nil {
		return nil, PreparedLiveEgress{}, err
	}
	p, err := s.CheckOwnLiveAttempt(ctx, a, operation, c, projector)
	if err != nil || !c.Current(ticket) || ctx.Err() != nil || !gate.InferenceEnabled(ctx) {
		return nil, PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	return &nativeLiveDispatch{store: s, access: a, operation: operation, controller: c,
		ticket: ticket, projector: projector, gate: gate}, p, nil
}

func (h *nativeLiveDispatch) current(ctx context.Context) (PreparedLiveEgress, error) {
	if h == nil || h.store == nil || h.controller == nil || h.gate == nil || ctx == nil || ctx.Err() != nil || !h.controller.Current(h.ticket) || !h.gate.InferenceEnabled(ctx) {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p, err := h.store.CheckOwnLiveAttempt(ctx, h.access, h.operation, h.controller, h.projector)
	if err != nil || ctx.Err() != nil || !h.controller.Current(h.ticket) || !h.gate.InferenceEnabled(ctx) {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	return p, nil
}

func nativeLiveRequestEqual(a, b modelgateway.Request) bool {
	first, e1 := json.Marshal(a)
	second, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && len(first) <= modelgateway.MaxRequestBytes && string(first) == string(second)
}

type nativeLiveModelDispatch struct{ nativeLiveDispatch }

var _ modelgateway.NativeLiveDispatchPort = (*nativeLiveModelDispatch)(nil)

// NewOwnLiveModelDispatch binds the controlled Gateway to one original native
// operation. Calling it again cannot revive an operation already IN_FLIGHT or
// UNKNOWN: BeginOwnLiveAttempt is the single durable RESERVED -> IN_FLIGHT CAS.
func (s *Store) NewOwnLiveModelDispatch(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, projector LiveWireProjector, gate modelgateway.LiveGate) (modelgateway.NativeLiveDispatchPort, modelgateway.Request, error) {
	h, p, err := s.newNativeLiveDispatch(ctx, a, operation, c, projector, gate)
	if err != nil {
		return nil, modelgateway.Request{}, err
	}
	r := p.Request()
	w := p.ModelWire()
	if p.Kind() != modelegressbudget.LiveToken || r.Budget.MaxOutputTokens < 1 || r.Budget.MaxOutputTokens > modelgateway.TencentLiveMaxOutputTokens || !w.Matches(nativeProviderRequest(r), time.Now()) {
		return nil, modelgateway.Request{}, modelegressbudget.ErrDenied
	}
	// Copy fields individually: atomic guards are never copied after use.
	model := &nativeLiveModelDispatch{nativeLiveDispatch: nativeLiveDispatch{
		store: h.store, access: h.access, operation: h.operation, controller: h.controller,
		ticket: h.ticket, projector: h.projector, gate: h.gate}}
	return model, r, nil
}

func nativeProviderRequest(r modelgateway.Request) modelgateway.ProviderRequest {
	return modelgateway.ProviderRequest{TaskKind: r.TaskKind, PromptVersion: r.PromptVersion,
		OutputMode: r.OutputMode, OutputSchemaVersion: r.OutputSchemaVersion,
		Messages:      append([]modelgateway.Message(nil), r.Messages...),
		ToolAllowlist: append([]string(nil), r.ToolAllowlist...), MaxOutputTokens: r.Budget.MaxOutputTokens,
		DeadlineAt: r.DeadlineAt}
}

func (h *nativeLiveModelDispatch) CheckCurrent(ctx context.Context, r modelgateway.Request, wire modelgateway.PreparedTencentWire) error {
	if h == nil {
		return modelegressbudget.ErrDenied
	}
	p, err := h.current(ctx)
	if err != nil || !nativeLiveRequestEqual(p.Request(), r) || wire.WireDigest() != p.ModelWire().WireDigest() || !wire.Matches(nativeProviderRequest(r), time.Now()) {
		return modelegressbudget.ErrDenied
	}
	return nil
}

func (h *nativeLiveModelDispatch) ReleaseOnce(ctx context.Context, r modelgateway.Request, wire modelgateway.PreparedTencentWire, invoke func(context.Context) ([]byte, error)) ([]byte, error) {
	if h == nil || invoke == nil || !h.used.CompareAndSwap(false, true) || h.CheckCurrent(ctx, r, wire) != nil {
		return nil, modelegressbudget.ErrDenied
	}
	p, err := h.store.BeginOwnLiveAttempt(ctx, h.access, h.operation, h.controller, h.projector)
	// No callback after a failed or uncertain commit, including a lost commit
	// response. Native Begin is durable; the process cannot assume it rolled back.
	if err != nil || !nativeLiveRequestEqual(p.Request(), r) || p.ModelWire().WireDigest() != wire.WireDigest() || h.CheckCurrent(ctx, r, wire) != nil {
		return nil, modelegressbudget.ErrDenied
	}
	return invoke(ctx)
}

func (h *nativeLiveModelDispatch) SettleUnknown(ctx context.Context, r modelgateway.Request, wire modelgateway.PreparedTencentWire) error {
	if h == nil || h.store == nil || ctx == nil || ctx.Err() != nil || !h.used.Load() || wire.WireDigest() == "" {
		return modelegressbudget.ErrDenied
	}
	// Accounting is deliberately independent of obsolete Task/source authority.
	// Finish authenticates the current owner and retains the entire existing
	// hold. Current output authority is checked separately by CheckCurrent.
	_, err := h.store.FinishOwnLiveAttempt(ctx, h.access, h.operation,
		modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"})
	return err
}

// OwnLiveSearchOutput is server-local untrusted search content. It is not a
// MODEL_EGRESS grant, Entity, coordinate or native ResultSet projection. JSON
// reconstruction cannot turn provider snippets into a source export authority.
type OwnLiveSearchOutput struct {
	result     agenttool.TencentWSAResult
	query      string
	prepared   PreparedLiveEgress
	operation  string
	access     agentevent.Access
	observedAt time.Time
}

func (OwnLiveSearchOutput) MarshalJSON() ([]byte, error) { return nil, modelegressbudget.ErrServerOnly }
func (*OwnLiveSearchOutput) UnmarshalJSON([]byte) error  { return modelegressbudget.ErrServerOnly }
func (OwnLiveSearchOutput) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "OwnLiveSearchOutput{redacted}")
}
func (o OwnLiveSearchOutput) Result() agenttool.TencentWSAResult {
	r := o.result
	r.Sources = append([]agenttool.TencentWSASource(nil), r.Sources...)
	return r
}

// ExecuteOwnLiveSearch sends only the current native scalar query after the
// original reservation's commit. No auto retry, source-page/media fetch or
// model call follows. The returned sources still require a separate authorized
// export contract before they may be included in any model input.
func (s *Store) ExecuteOwnLiveSearch(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, gate modelgateway.LiveGate, adapter *agenttool.TencentWSAAdapter) (OwnLiveSearchOutput, error) {
	var empty OwnLiveSearchOutput
	if adapter == nil {
		return empty, modelegressbudget.ErrUnavailable
	}
	h, p, err := s.newNativeLiveDispatch(ctx, a, operation, c, nil, gate)
	if err != nil || p.Kind() != modelegressbudget.LiveCall || p.SearchQuery() == "" {
		return empty, modelegressbudget.ErrDenied
	}
	if !h.used.CompareAndSwap(false, true) {
		return empty, modelegressbudget.ErrDenied
	}
	p, err = s.BeginOwnLiveAttempt(ctx, a, operation, c, nil)
	if err != nil {
		return empty, err
	}
	deadline := p.DeadlineAt()
	if until := time.Now().Add(agenttool.TencentWSAMaxDeadline); deadline.After(until) {
		deadline = until
	}
	callctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var result agenttool.TencentWSAResult
	if _, err = h.current(callctx); err == nil {
		result, err = adapter.SearchWithNativeGuard(callctx, p.SearchQuery(), deadline, func(currentCtx context.Context) error {
			_, currentErr := h.current(currentCtx)
			return currentErr
		})
	}
	_, currentErr := h.current(callctx)
	accountCtx, accountCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	_, accountErr := s.FinishOwnLiveAttempt(accountCtx, a, operation,
		modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"})
	accountCancel()
	if currentErr != nil || accountErr != nil {
		return empty, modelegressbudget.ErrDenied
	}
	if err != nil {
		return empty, err
	}
	if result.CashStatus != "UNKNOWN" || result.RequestID == "" {
		return empty, modelegressbudget.ErrDenied
	}
	return OwnLiveSearchOutput{result: result, query: p.SearchQuery(), prepared: p,
		operation: operation, access: a, observedAt: time.Now().UTC().Truncate(time.Microsecond)}, nil
}
