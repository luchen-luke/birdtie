package postgres

// AIR010 exercises the real 062 approval/budget and 088 ModelRequestRun ports.
// Adapters stay LOCAL_SYNTHETIC; these tests never send a network request.
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
)

func resilienceRun(t *testing.T, f *egressFixture, ctx context.Context, id string, policy modelresilience.Policy, steps []modelegressbudget.LocalRetryStep) (modelegressbudget.LocalRetryOutcome, error) {
	t.Helper()
	r := modelegressbudget.NewLocalModelRunRunner(f.f.native.private.base.store, f.gate, policy)
	if r == nil {
		t.Fatal("valid native resilience runner unavailable")
	}
	return r.Run(ctx, f.f.native.access, id, steps)
}

type resilienceLateAdapter struct{ *nativeRetryAdapter }

func (a *resilienceLateAdapter) Complete(ctx context.Context, r modelgateway.ProviderRequest) ([]byte, error) {
	// The actual adapter is reached before this barrier. Ignore its cancellation
	// and return afterwards, rather than assuming native setup fits 100ms.
	<-ctx.Done()
	time.Sleep(20 * time.Millisecond)
	return a.nativeRetryAdapter.Complete(ctx, r)
}

func resilienceStep(t *testing.T, f *egressFixture, p modelegressbudget.Preview, a modelegressbudget.LocalAttemptAdapter) modelegressbudget.LocalRetryStep {
	t.Helper()
	d := modelegressbudget.NewLocalAttemptDriver(f.f.native.private.base.store, a, f.gate)
	if d == nil {
		t.Fatal("native local resilience adapter invalid")
	}
	return modelegressbudget.LocalRetryStep{Driver: d, Input: nativeAttemptInput(t, f, p)}
}

func resilienceControl(t *testing.T, f *egressFixture, id string, steps []modelegressbudget.LocalRetryStep, called int) modelrequestrun.Control {
	t.Helper()
	b := f.f.native.private.base
	c, e := b.store.ReadOwnLocalModelRun(b.ctx, f.f.native.access, id)
	if e != nil || len(c.Steps) != len(steps) || c.TaskID != f.f.native.task.ID || c.RootTraceID != f.root || c.BindingID != f.binding || c.ModelRunID != id {
		t.Fatal("original Run identity/control changed", e)
	}
	for i, s := range c.Steps {
		if s.OperationID != steps[i].Input.OperationID || s.PreviewID != steps[i].Input.PreviewID {
			t.Fatal("original operation/approved preview changed")
		}
		if i >= called && (s.State != modelrequestrun.StepPlanned || s.ReservationID != "") {
			t.Fatal("unused attempt reserved or dispatched", s.State)
		}
	}
	var reservations int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&reservations); e != nil || reservations != called {
		t.Fatal("native reservation count differs from actual attempts", reservations, called, e)
	}
	retryAssertOriginalBudgets(t, f, int64(called))
	wire, e := json.Marshal(c)
	if e != nil || strings.Contains(string(wire), f.f.native.task.Query) || strings.Contains(string(wire), "本地合成答案") || strings.Contains(string(wire), "messages") {
		t.Fatal("control persisted private query/result")
	}
	t.Logf("LOCAL_SYNTHETIC run=%s originalTask=%s root=%s binding=%s called=%d state=%s", id, c.TaskID, c.RootTraceID, c.BindingID, called, c.State)
	return c
}

func TestModelResilienceNativeRetryAfterJitterFallbackAndHeldUnknownCost(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	alt := retryAlternatePreview(t, f, "fallback", true)
	a := &nativeRetryAdapter{failure: modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}, 140*time.Millisecond)}
	next := &nativeRetryAdapter{}
	next.model = "fallback"
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, a), retryNativeStep(t, f, b.store, alt, next)}
	policy := retryNativePolicy()
	policy.BaseBackoff = 100 * time.Millisecond
	policy.MaxBackoff = 300 * time.Millisecond
	policy.JitterPermille = 200
	policy.SameRouteAttempts = 1
	policy.MaxProviderSwitches = 1
	id := modelRunID(t, f)
	o, e := resilienceRun(t, f, b.ctx, id, policy, steps)
	if e != nil || len(o.EncodedResult) == 0 || len(o.Attempts) != 2 || a.calls.Load() != 1 || next.calls.Load() != 1 {
		t.Fatal("native approved fallback failed", e)
	}
	// Actual elapsed includes original native revalidation and accounting. The
	// upper scheduling bound is separately exact arithmetic, not a latency SLA.
	elapsed := next.times[0].Sub(a.times[0])
	if elapsed < 140*time.Millisecond {
		t.Fatal("actual fallback preceded provider Retry-After", elapsed)
	}
	for _, unit := range []int{0, 1, 499, 1000} {
		d, err := modelresilience.RetryDelay(policy, 1, 140*time.Millisecond, true, unit)
		if err != nil || d < 140*time.Millisecond || d > policy.MaxBackoff {
			t.Fatal("bounded positive jitter violated provider lower bound", d, err)
		}
	}
	c := resilienceControl(t, f, id, steps, 2)
	if c.State != modelrequestrun.RunFinished || c.Steps[0].State != modelrequestrun.StepUnknown || c.Steps[1].State != modelrequestrun.StepSettled || c.Steps[0].RequestDigest == c.Steps[1].RequestDigest {
		t.Fatal("fallback failed to retain distinct exact approvals/fee states")
	}
	var state string
	var settledCost *int64
	var upperCost int64
	if e = b.pool.QueryRow(b.ctx, `SELECT state,settled_cost,upper_cost FROM model_budget_reservations WHERE operation_id=$1`, steps[0].Input.OperationID).Scan(&state, &settledCost, &upperCost); e != nil || state != "UNKNOWN" || upperCost <= 0 || settledCost != nil {
		t.Fatal("unknown failed-attempt fee must retain original upper bound with unknown settled cost", e, state, settledCost, upperCost)
	}
	if again, err := resilienceRun(t, f, b.ctx, id, policy, steps); !errors.Is(err, modelegressbudget.ErrConflict) || len(again.EncodedResult) != 0 || a.calls.Load() != 1 || next.calls.Load() != 1 {
		t.Fatal("original run replayed or regenerated private answer", err)
	}
	t.Logf("provider Retry-After=140ms actual inter-call=%s scheduling jitter bounds tested independently", elapsed)
}

func TestModelResilienceNativeTerminalFaultMatrixNeverFallback(t *testing.T) {
	for _, kind := range []string{"authentication", "invalidRequest", "refusedError", "unknownDispatch", "invalidHint", "excessiveHint", "timeoutLateError", "timeoutLateRaw", "cancel", "invalidSchema", "refusedResult", "truncatedResult"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", true)
			a := &nativeRetryAdapter{}
			var adapter modelegressbudget.LocalAttemptAdapter = a
			next := &nativeRetryAdapter{}
			next.model = "fallback"
			policy := retryNativePolicy()
			policy.SameRouteAttempts = 1
			policy.MaxProviderSwitches = 1
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			switch kind {
			case "authentication":
				a.failure = modelgateway.ProviderError{Code: "AUTHENTICATION", Retryable: true}
			case "invalidRequest":
				a.failure = modelgateway.ProviderError{Code: "INVALID_REQUEST", Retryable: true}
			case "refusedError":
				a.failure = modelgateway.ProviderError{Code: "REFUSED", Retryable: true}
			case "unknownDispatch":
				a.failure = errors.New("synthetic unknown delivery; no response authority")
			case "invalidHint":
				a.failure = modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT"}, -1)
			case "excessiveHint":
				a.failure = modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT"}, policy.MaxBackoff+time.Second)
			case "timeoutLateError":
				policy.AttemptTimeout = 2 * time.Second
				adapter = &resilienceLateAdapter{a}
				a.failure = modelgateway.ProviderError{Code: "TEMPORARY"}
			case "timeoutLateRaw":
				policy.AttemptTimeout = 2 * time.Second
				adapter = &resilienceLateAdapter{a}
			case "cancel":
				a.onCall = cancel
				a.failure = modelgateway.ProviderError{Code: "RATE_LIMIT"}
			case "invalidSchema":
				a.raw = []byte(`{"status":"COMPLETED","request_id":"local","finish_reason":"stop","structured":{"unapproved":"value"},"usage":{"status":"UNKNOWN"}}`)
			case "refusedResult":
				a.raw = []byte(`{"status":"REFUSED","request_id":"local","finish_reason":"refusal","usage":{"status":"UNKNOWN"}}`)
			case "truncatedResult":
				a.raw = []byte(`{"status":"TRUNCATED","request_id":"local","finish_reason":"length","usage":{"status":"UNKNOWN"}}`)
			}
			steps := []modelegressbudget.LocalRetryStep{resilienceStep(t, f, p, adapter), retryNativeStep(t, f, b.store, alt, next)}
			id := modelRunID(t, f)
			o, e := resilienceRun(t, f, ctx, id, policy, steps)
			if a.calls.Load() != 1 || next.calls.Load() != 0 {
				t.Fatal("terminal/unknown request switched provider", kind, e, a.calls.Load(), next.calls.Load())
			}
			if kind != "refusedResult" && kind != "truncatedResult" && (e == nil || len(o.EncodedResult) != 0) {
				t.Fatal("late/invalid/unknown result released", kind, e)
			}
			if (kind == "refusedResult" || kind == "truncatedResult") && (e != nil || len(o.EncodedResult) == 0) {
				t.Fatal("valid terminal refusal/truncation not preserved", kind, e)
			}
			if len(o.EncodedResult) > 0 {
				var r modelgateway.Result
				if json.Unmarshal(o.EncodedResult, &r) != nil || (r.Status != "REFUSED" && r.Status != "TRUNCATED") {
					t.Fatal("terminal response masqueraded as completed")
				}
			}
			resilienceControl(t, f, id, steps, 1)
		})
	}
}

func TestModelResilienceNativeSourceAndTicketRetireDuringAttempt(t *testing.T) {
	for _, kind := range []string{"taskABA", "withdrawB", "gateOffOn", "sessionRevoke", "sessionExpiry", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", true)
			a := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
			next := &nativeRetryAdapter{}
			next.model = "fallback"
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			a.onCall = func() {
				switch kind {
				case "taskABA":
					b.exec(`UPDATE agent_tasks SET query=query||' changed' WHERE id=$1`, p.TaskID)
					b.exec(`UPDATE agent_tasks SET query=$2 WHERE id=$1`, p.TaskID, f.f.native.task.Query)
				case "withdrawB":
					if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, alt.ID); e != nil {
						t.Fatal(e)
					}
				case "gateOffOn":
					retryGateRestore(t, f)
				case "sessionRevoke":
					b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.f.native.private.ownerSession)
				case "sessionExpiry":
					b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '150 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
				case "cancel":
					cancel()
				}
			}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, a), retryNativeStep(t, f, b.store, alt, next)}
			id := modelRunID(t, f)
			policy := retryNativePolicy()
			policy.BaseBackoff = 300 * time.Millisecond
			policy.MaxBackoff = time.Second
			policy.SameRouteAttempts = 1
			policy.MaxProviderSwitches = 1
			o, e := resilienceRun(t, f, ctx, id, policy, steps)
			if e == nil || len(o.EncodedResult) != 0 || a.calls.Load() != 1 || next.calls.Load() != 0 {
				t.Fatal("retired plan dispatched fallback or released answer", kind, e)
			}
			// Session revocation/expiry correctly deny even own control APIs. Inspect
			// only this random fixture's native rows, never bypassing the runtime.
			var n int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&n); e != nil || n != 1 {
				t.Fatal("unexpected second reservation", n, e)
			}
			var state string
			if e = b.pool.QueryRow(b.ctx, `SELECT state FROM model_request_run_steps WHERE run_id=$1 AND ordinal=2`, id).Scan(&state); e != nil || state != "PLANNED" {
				t.Fatal("unused fallback mutated", state, e)
			}
		})
	}
}

// This wrapper never supplies authorization: every port method, including the
// failure check, calls the actual native handle first. The hook changes real
// source state only after that check, while the original backoff is running.
type resilienceRunHookPort struct {
	modelegressbudget.LocalModelRunPort
	modelegressbudget.LocalRetryPort
	afterFailure func()
	once         sync.Once
}
type resilienceRunHookHandle struct {
	modelegressbudget.LocalModelRunHandle
	port *resilienceRunHookPort
}

func (p *resilienceRunHookPort) CreateOwnLocalModelRun(ctx context.Context, a agentevent.Access, id string, b []modelegressbudget.LocalRetryBinding, g *agentfeature.Controller, ticket agentfeature.Ticket) (modelegressbudget.LocalModelRunHandle, modelrequestrun.Control, error) {
	h, c, e := p.LocalModelRunPort.CreateOwnLocalModelRun(ctx, a, id, b, g, ticket)
	if e != nil {
		return nil, c, e
	}
	return &resilienceRunHookHandle{h, p}, c, nil
}
func (h *resilienceRunHookHandle) CheckOwnLocalRetryFailure(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, f modelegressbudget.LocalRetryFailure, g *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := h.LocalModelRunHandle.CheckOwnLocalRetryFailure(ctx, a, p, f, g)
	if e == nil {
		h.port.once.Do(h.port.afterFailure)
	}
	return cp, e
}
func TestModelResilienceNativeActualBackoffRechecksOriginalSource(t *testing.T) {
	for _, kind := range []string{"withdrawB", "taskABA", "gateOffOn", "sessionExpiry", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", true)
			ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
			defer cancel()
			changed := make(chan error, 1)
			port := &resilienceRunHookPort{LocalModelRunPort: b.store, LocalRetryPort: b.store}
			port.afterFailure = func() {
				go func() {
					timer := time.NewTimer(60 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-ctx.Done():
						changed <- ctx.Err()
						return
					case <-timer.C:
					}
					var e error
					switch kind {
					case "withdrawB":
						e = b.store.RevokeOwnModelEgress(ctx, f.f.native.access, alt.ID)
					case "taskABA":
						_, e = b.pool.Exec(ctx, `UPDATE agent_tasks SET query=query||' changed' WHERE id=$1`, p.TaskID)
						if e == nil {
							_, e = b.pool.Exec(ctx, `UPDATE agent_tasks SET query=$2 WHERE id=$1`, p.TaskID, f.f.native.task.Query)
						}
					case "gateOffOn":
						retryGateRestore(t, f)
					case "sessionExpiry":
						_, e = b.pool.Exec(ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '20 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
						time.Sleep(35 * time.Millisecond)
					case "cancel":
						cancel()
					}
					changed <- e
				}()
			}
			a := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "RATE_LIMIT"}}
			next := &nativeRetryAdapter{}
			next.model = "fallback"
			mk := func(preview modelegressbudget.Preview, adapter modelegressbudget.LocalAttemptAdapter) modelegressbudget.LocalRetryStep {
				d := modelegressbudget.NewLocalAttemptDriver(port, adapter, f.gate)
				if d == nil {
					t.Fatal("hooked native driver invalid")
				}
				return modelegressbudget.LocalRetryStep{Driver: d, Input: nativeAttemptInput(t, f, preview)}
			}
			steps := []modelegressbudget.LocalRetryStep{mk(p, a), mk(alt, next)}
			id := modelRunID(t, f)
			policy := retryNativePolicy()
			policy.BaseBackoff = 350 * time.Millisecond
			policy.SameRouteAttempts = 1
			policy.MaxProviderSwitches = 1
			r := modelegressbudget.NewLocalModelRunRunner(port, f.gate, policy)
			if r == nil {
				t.Fatal("hooked runner invalid")
			}
			o, e := r.Run(ctx, f.f.native.access, id, steps)
			select {
			case changeErr := <-changed:
				if changeErr != nil {
					t.Fatal("actual backoff source fixture failed", changeErr)
				}
			case <-time.After(time.Second):
				t.Fatal("native failure checkpoint hook never ran; not a successful backoff test")
			}
			if e == nil || len(o.EncodedResult) != 0 || a.calls.Load() != 1 || next.calls.Load() != 0 {
				t.Fatal("current source not rechecked after actual backoff", kind, e)
			}
			var n int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&n); e != nil || n != 1 {
				t.Fatal("backoff made another reservation", n, e)
			}
			t.Log("real native failure revalidation preceded source mutation during 350ms backoff; fallback calls=0")
		})
	}
}

func TestModelResilienceNativeSharedRootAndAttemptBudgetStop(t *testing.T) {
	for _, kind := range []string{"rootExhaustion", "attemptsExhaustion"} {
		t.Run(kind, func(t *testing.T) {
			limit := int64(4)
			if kind == "rootExhaustion" {
				limit = 1
			}
			f := newEgressFixture(t, limit)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			a := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
			next := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "RATE_LIMIT"}}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, a), retryNativeStep(t, f, b.store, p, next)}
			id := modelRunID(t, f)
			policy := retryNativePolicy()
			o, e := resilienceRun(t, f, b.ctx, id, policy, steps)
			if !errors.Is(e, modelegressbudget.ErrBudget) || len(o.EncodedResult) != 0 {
				t.Fatal("finite native budget not enforced", kind, e)
			}
			called := 1
			if kind == "attemptsExhaustion" {
				called = 2
			}
			if a.calls.Load() != 1 || next.calls.Load() != int32(called-1) {
				t.Fatal("budget dispatch count mismatch")
			}
			resilienceControl(t, f, id, steps, called)
			if kind == "rootExhaustion" {
				fresh := &nativeRetryAdapter{}
				more := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, fresh)}
				secondID := modelRunID(t, f)
				if again, err := resilienceRun(t, f, b.ctx, secondID, policy, more); !errors.Is(err, modelegressbudget.ErrBudget) || len(again.EncodedResult) != 0 || fresh.calls.Load() != 0 {
					t.Fatal("different Run bypassed original shared root ceiling", err)
				}
				retryAssertOriginalBudgets(t, f, 1)
			}
		})
	}
}

func TestModelResilienceNativeInitialUnapprovedAndDefaultOffHaveZeroEffects(t *testing.T) {
	for _, kind := range []string{"off", "unapprovedB", "wrongRegion", "wrongRetention", "wrongWire", "wrongPreview", "noSwitch"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", kind != "unapprovedB")
			a := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "RATE_LIMIT"}}
			next := &nativeRetryAdapter{}
			next.model = "fallback"
			policy := retryNativePolicy()
			policy.SameRouteAttempts = 1
			policy.MaxProviderSwitches = 1
			switch kind {
			case "off":
				if e := f.gate.Disable(agentfeature.Enrichment); e != nil {
					t.Fatal(e)
				}
			case "wrongRegion":
				d := next.LocalDestination()
				d.Region = "EU"
				next.destinationOverride = &d
			case "wrongRetention":
				d := next.LocalDestination()
				d.Retention = "UNKNOWN"
				next.destinationOverride = &d
			case "wrongWire":
				d := next.LocalDestination()
				d.Key.WireContract = "other.v1"
				next.destinationOverride = &d
			case "wrongPreview":
				alt.ID = egressID(t, f)
			case "noSwitch":
				policy.MaxProviderSwitches = 0
			}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, a), retryNativeStep(t, f, b.store, alt, next)}
			id := modelRunID(t, f)
			o, e := resilienceRun(t, f, b.ctx, id, policy, steps)
			if e == nil || len(o.EncodedResult) != 0 || a.calls.Load() != 0 || next.calls.Load() != 0 {
				t.Fatal("unapproved route/gate dispatched", kind, e)
			}
			var n int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&n); e != nil || n != 0 {
				t.Fatal("invalid initial plan consumed budget", n, e)
			}
		})
	}
}

func TestModelResilienceNativeNoSwitchSameRouteAndUnusedFallback(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "unused", true: "sameRouteRetry"}[retry], func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			a := &nativeRetryAdapter{}
			next := &nativeRetryAdapter{}
			if retry {
				a.failure = modelgateway.ProviderError{Code: "TEMPORARY"}
			}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, a), retryNativeStep(t, f, b.store, p, next)}
			id := modelRunID(t, f)
			policy := retryNativePolicy()
			policy.SameRouteAttempts = 1
			policy.MaxProviderSwitches = 0
			o, e := resilienceRun(t, f, b.ctx, id, policy, steps)
			if e != nil || len(o.EncodedResult) == 0 {
				t.Fatal("valid same-route native run rejected", e)
			}
			called := 1
			if retry {
				called = 2
			}
			resilienceControl(t, f, id, steps, called)
			if a.calls.Load() != 1 || next.calls.Load() != int32(called-1) {
				t.Fatal("incorrect actual calls")
			}
		})
	}
}
