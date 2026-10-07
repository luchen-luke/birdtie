package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
	"github.com/jackc/pgx/v5/pgxpool"
)

type nativeRetryAdapter struct {
	nativeLocalAttemptAdapter
	failure       error
	onCall        func()
	onDestination func()
	mu            sync.Mutex
	times         []time.Time
	seenDeadlines []time.Time
}

func (a *nativeRetryAdapter) LocalDestination() modelegressbudget.LocalAttemptDestination {
	d := a.nativeLocalAttemptAdapter.LocalDestination()
	if a.onDestination != nil {
		a.onDestination()
	}
	return d
}

func (a *nativeRetryAdapter) Complete(ctx context.Context, r modelgateway.ProviderRequest) ([]byte, error) {
	a.mu.Lock()
	a.times = append(a.times, time.Now())
	deadline, _ := ctx.Deadline()
	a.seenDeadlines = append(a.seenDeadlines, deadline)
	a.mu.Unlock()
	if a.onCall != nil {
		a.onCall()
	}
	if a.failure != nil {
		a.calls.Add(1)
		return nil, a.failure
	}
	return a.nativeLocalAttemptAdapter.Complete(ctx, r)
}

func retryGateRestore(t *testing.T, f *egressFixture) {
	t.Helper()
	if e := f.gate.Disable(agentfeature.Enrichment); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": map[string]bool{"agent_enrichment": true, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}, "pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
	cfg, e := agentfeature.ParseConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.gate.Replace(f.gate.Revision(), cfg); e != nil {
		t.Fatal(e)
	}
}
func TestModelEgressLocalRetryNativeOriginalTicketCapturedBeforeAdapterGetters(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	a := &nativeRetryAdapter{}
	step := retryNativeStep(t, f, b.store, p, a)
	var once atomic.Bool
	a.onDestination = func() {
		if once.CompareAndSwap(false, true) {
			retryGateRestore(t, f)
		}
	}
	out, e := retryNativeRun(t, f, b.store, []modelegressbudget.LocalRetryStep{step}, retryNativePolicy())
	if !errors.Is(e, modelegressbudget.ErrUnavailable) || a.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("getter revived round ticket", e)
	}
	retryAssertOriginalBudgets(t, f, 0)
}

func TestModelEgressLocalRetryNativeFullPlanDispatchPoolWait(t *testing.T) {
	for _, kind := range []string{"revokeB", "taskABA", "sessionExpiry"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", true)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.MaxConns = 1
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			worker := New(pool, false)
			input := nativeAttemptInput(t, f, p)
			other := nativeAttemptInput(t, f, alt)
			a := &nativeRetryAdapter{}
			fallback := &nativeRetryAdapter{}
			fallback.model = "fallback"
			bindings := []modelegressbudget.LocalRetryBinding{{Input: input, Destination: a.LocalDestination()}, {Input: other, Destination: fallback.LocalDestination()}}
			proof, e := worker.CaptureOwnLocalRetryPlan(ctx, f.f.native.access, bindings, f.gate)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = worker.ReserveOwnModelAttempt(ctx, f.f.native.access, input, f.gate); e != nil {
				t.Fatal(e)
			}
			request, e := worker.BeginOwnLocalModelAttempt(ctx, f.f.native.access, input.OperationID, f.gate)
			if e != nil {
				t.Fatal(e)
			}
			hold, e := pool.Acquire(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Release()
			before := pool.Stat().EmptyAcquireCount()
			done := make(chan error, 1)
			go func() {
				_, e := worker.CheckOwnLocalRetryDispatch(ctx, f.f.native.access, proof, input.OperationID, request, a.LocalDestination(), f.gate)
				done <- e
			}()
			select {
			case e := <-done:
				t.Fatal("dispatch did not wait for exclusive pool", e)
			case <-time.After(30 * time.Millisecond):
			}
			switch kind {
			case "revokeB":
				if e = b.store.RevokeOwnModelEgress(ctx, f.f.native.access, alt.ID); e != nil {
					t.Fatal(e)
				}
			case "taskABA":
				b.exec(`UPDATE agent_tasks SET query=query||' changed' WHERE id=$1`, p.TaskID)
				b.exec(`UPDATE agent_tasks SET query=$2 WHERE id=$1`, p.TaskID, p.Request.Messages[len(p.Request.Messages)-1].Content)
			case "sessionExpiry":
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '35 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
				time.Sleep(60 * time.Millisecond)
			}
			hold.Release()
			select {
			case e = <-done:
			case <-ctx.Done():
				t.Fatal("dispatch did not finish", ctx.Err())
			}
			want := modelegressbudget.ErrDenied
			if !errors.Is(e, want) || pool.Stat().EmptyAcquireCount() <= before {
				t.Fatal("full original plan revived after actual pool wait", kind, e)
			}
			if a.calls.Load() != 0 || fallback.calls.Load() != 0 {
				t.Fatal("read fence dispatched adapter")
			}
		})
	}
}

func TestModelEgressLocalRetryNativeConcurrentOriginalPlanOneDispatch(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	a := &nativeRetryAdapter{}
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, a)}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out, e := retryNativeRun(t, f, b.store, steps, retryNativePolicy())
			if e == nil {
				if len(out.EncodedResult) == 0 {
					t.Error("empty result")
				}
				success.Add(1)
			} else if !errors.Is(e, modelegressbudget.ErrConflict) {
				t.Error(e)
			}
		}()
	}
	close(start)
	wg.Wait()
	if success.Load() != 1 || a.calls.Load() != 1 {
		t.Fatal("same original plan dispatched twice", success.Load(), a.calls.Load())
	}
	retryAssertOriginalBudgets(t, f, 1)
}

func TestModelEgressLocalRetryNativeTightenedDelegateDeadlineCannotRefresh(t *testing.T) {
	for _, kind := range []string{"rateLimit", "success"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			first := &nativeRetryAdapter{}
			if kind == "rateLimit" {
				first.failure = modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}
			}
			var tightened atomic.Bool
			port := &nativeRetryHookPort{LocalRetryPort: b.store, afterDispatch: func() {
				if tightened.CompareAndSwap(false, true) {
					b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '250 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
				}
			}}
			first.onCall = func() {
				// A legitimate idle refresh must not revive the earlier native
				// bound already passed to this actual delegate context.
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.f.native.private.ownerSession)
				time.Sleep(300 * time.Millisecond)
			}
			next := &nativeRetryAdapter{}
			out, e := retryNativeRun(t, f, port, []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, first), retryNativeStep(t, f, port, p, next)}, retryNativePolicy())
			if !tightened.Load() || e == nil || first.calls.Load() != 1 || next.calls.Load() != 0 || len(out.EncodedResult) != 0 {
				t.Fatal("short native delegate deadline revived by later idle refresh", kind, e, first.calls.Load(), next.calls.Load(), len(out.EncodedResult))
			}
			first.mu.Lock()
			actual := first.seenDeadlines[0]
			first.mu.Unlock()
			if !time.Now().After(actual) {
				t.Fatal("fixture did not cross actual shortened context", actual)
			}
		})
	}
}

func TestModelEgressLocalRetryNativeObservedShortBoundMustReachNextAttempt(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}}
	var tightened atomic.Bool
	port := &nativeRetryHookPort{LocalRetryPort: b.store, afterDispatch: func() {
		if tightened.CompareAndSwap(false, true) {
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '300 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
		}
	}}
	first.onCall = func() {
		b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.f.native.private.ownerSession)
	}
	next := &nativeRetryAdapter{}
	policy := retryNativePolicy()
	policy.BaseBackoff = 400 * time.Millisecond
	out, e := retryNativeRun(t, f, port, []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, first), retryNativeStep(t, f, port, p, next)}, policy)
	if !tightened.Load() || e == nil || first.calls.Load() != 1 || next.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("previously observed short bound discarded before next attempt", e, first.calls.Load(), next.calls.Load(), len(out.EncodedResult))
	}
	retryAssertOriginalBudgets(t, f, 1)
}

type nativeRetrySettleDelayPort struct {
	modelegressbudget.LocalRetryPort
	delay time.Duration
}

type nativeOnceDispatchArmPort struct {
	modelegressbudget.LocalRetryPort
	arm func()
}

func (p *nativeOnceDispatchArmPort) CheckOwnLocalModelAttemptDispatch(ctx context.Context, a agentevent.Access, op string, g *agentfeature.Controller, req modelgateway.Request, dst modelegressbudget.LocalAttemptDestination) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := p.LocalRetryPort.CheckOwnLocalModelAttemptDispatch(ctx, a, op, g, req, dst)
	if e == nil {
		p.arm()
	}
	return cp, e
}

func TestModelEgressLocalRetryNativeStandaloneGetterAfterDispatchCannotRevokeAndCall(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	var armed, revoked atomic.Bool
	port := &nativeOnceDispatchArmPort{LocalRetryPort: b.store, arm: func() { armed.Store(true) }}
	a := &nativeRetryAdapter{}
	step := retryNativeStep(t, f, port, p, a)
	a.onDestination = func() {
		if armed.Load() && revoked.CompareAndSwap(false, true) {
			if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); e != nil {
				t.Fatal(e)
			}
		}
	}
	out, e := step.Driver.Once(b.ctx, f.f.native.access, step.Input)
	if !revoked.Load() || !errors.Is(e, modelegressbudget.ErrDenied) || a.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("standalone getter revoked original preview but handed over query", e, a.calls.Load())
	}
	retryAssertOriginalBudgets(t, f, 1)
}

func (p *nativeRetrySettleDelayPort) SettleOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, op string, u modelegressbudget.LocalUsage) (modelegressbudget.Reservation, error) {
	r, e := p.LocalRetryPort.SettleOwnLocalModelAttempt(ctx, a, op, u)
	if e == nil {
		time.Sleep(p.delay)
	}
	return r, e
}

func TestModelEgressLocalRetryNativeStandaloneOnceKeepsDispatchShortBoundAtRelease(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	a := &nativeRetryAdapter{onCall: func() {
		b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.f.native.private.ownerSession)
	}}
	port := &nativeRetrySettleDelayPort{LocalRetryPort: b.store, delay: 500 * time.Millisecond}
	step := retryNativeStep(t, f, port, p, a)
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
	out, e := step.Driver.Once(b.ctx, f.f.native.access, step.Input)
	if !errors.Is(e, modelegressbudget.ErrDenied) || a.calls.Load() != 1 || len(out.EncodedResult) != 0 {
		t.Fatal("standalone Once released past original dispatch bound after idle refresh", e, a.calls.Load(), len(out.EncodedResult))
	}
	control, e := step.Driver.Recover(b.ctx, f.f.native.access, step.Input.OperationID)
	if e != nil || control.State != "SETTLED" {
		t.Fatal("deadline invented accounting rollback", control, e)
	}
	retryAssertOriginalBudgets(t, f, 1)
}
func TestModelEgressLocalRetryNativeSameRouteAllowedWhenNoSwitchAvailable(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
	next := &nativeRetryAdapter{}
	policy := retryNativePolicy()
	policy.MaxAttempts = 3
	policy.SameRouteAttempts = 1
	policy.MaxProviderSwitches = 0
	out, e := retryNativeRun(t, f, b.store, []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, p, next)}, policy)
	if e != nil || first.calls.Load() != 1 || next.calls.Load() != 1 || len(out.EncodedResult) == 0 {
		t.Fatal("legacy010 legal same-route policy rejected", e)
	}
	retryAssertOriginalBudgets(t, f, 2)
}
func TestModelEgressLocalRetryNativePriorShortestBDeadlineConstrainsActualAContext(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	alt := retryAlternatePreview(t, f, "fallback", true)
	// Create a separate explicit B preview with a shorter original deadline.
	short, e := b.store.PreviewOwnModelEgress(b.ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: p.TaskID, PriceVersion: alt.PriceVersion, MaxOutputTokens: 64, DeadlineAt: time.Now().UTC().Add(500 * time.Millisecond).Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	if e = b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, short.ID, short.RequestDigest); e != nil {
		t.Fatal(e)
	}
	first := &nativeRetryAdapter{onCall: func() { time.Sleep(550 * time.Millisecond) }}
	next := &nativeRetryAdapter{}
	next.model = "fallback"
	out, e := retryNativeRun(t, f, b.store, []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, short, next)}, retryNativePolicy())
	if e == nil || first.calls.Load() != 1 || next.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("A ignored prior B deadline", e, first.calls.Load())
	}
	if len(first.seenDeadlines) != 1 || first.seenDeadlines[0].After(short.ExpiresAt.Add(5*time.Millisecond)) {
		t.Fatal("transport context was not clamped to original B bound", first.seenDeadlines)
	}
}
func retryNativePolicy() modelresilience.Policy {
	p := modelresilience.DefaultPolicy()
	p.MaxAttempts = 4
	p.SameRouteAttempts = 4
	p.BaseBackoff = 60 * time.Millisecond
	p.MaxBackoff = time.Second
	p.JitterPermille = 0
	p.AttemptTimeout = 4 * time.Second
	p.MaxElapsed = 10 * time.Second
	return p
}
func retryNativeStep(t *testing.T, f *egressFixture, port modelegressbudget.LocalRetryPort, p modelegressbudget.Preview, a *nativeRetryAdapter) modelegressbudget.LocalRetryStep {
	t.Helper()
	d := modelegressbudget.NewLocalAttemptDriver(port, a, f.gate)
	if d == nil {
		t.Fatal("native retry adapter rejected")
	}
	return modelegressbudget.LocalRetryStep{Driver: d, Input: nativeAttemptInput(t, f, p)}
}
func retryNativeRun(t *testing.T, f *egressFixture, port modelegressbudget.LocalRetryPort, steps []modelegressbudget.LocalRetryStep, policy modelresilience.Policy) (modelegressbudget.LocalRetryOutcome, error) {
	t.Helper()
	r := modelegressbudget.NewLocalRetryRunner(port, f.gate, policy)
	if r == nil {
		t.Fatal("native retry runner rejected")
	}
	return r.Run(f.f.native.private.base.ctx, f.f.native.access, steps)
}
func retryAssertOriginalBudgets(t *testing.T, f *egressFixture, requests int64) {
	t.Helper()
	b := f.f.native.private.base
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, f.f.native.task.ID)
	if e != nil || len(views) != 4 {
		t.Fatal(views, e)
	}
	for _, v := range views {
		if v.Allocated.Requests != requests {
			t.Fatal("attempt budget count changed", v)
		}
	}
}

func TestModelEgressLocalRetryNativeExplicitResponsesEachReserveAndUnknownCostHeld(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	first := &nativeRetryAdapter{failure: modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT"}, 100*time.Millisecond)}
	second := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
	third := &nativeRetryAdapter{}
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, p, second), retryNativeStep(t, f, b.store, p, third)}
	out, e := retryNativeRun(t, f, b.store, steps, retryNativePolicy())
	if e != nil || len(out.EncodedResult) == 0 || len(out.Attempts) != 3 || first.calls.Load() != 1 || second.calls.Load() != 1 || third.calls.Load() != 1 {
		t.Fatal("three native attempts failed", e, out.ReasonCode)
	}
	if second.times[0].Sub(first.times[0]) < 100*time.Millisecond {
		t.Fatal("retry was earlier than provider lower bound")
	}
	retryAssertOriginalBudgets(t, f, 3)
	for i, step := range steps {
		control, e := b.store.ReadOwnLocalModelAttempt(b.ctx, f.f.native.access, step.Input.OperationID)
		want := "UNKNOWN"
		if i == 2 {
			want = "SETTLED"
		}
		if e != nil || control.State != want {
			t.Fatal(control, e)
		}
	}
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range views {
		if v.Allocated.InputTokens != 202 || v.Allocated.OutputTokens != 131 {
			t.Fatal("unknown costs/tokens refunded", v)
		}
	}
	// Restart/recovery cannot recreate or invoke a completed retry plan.
	again, e := retryNativeRun(t, f, b.store, steps, retryNativePolicy())
	if !errors.Is(e, modelegressbudget.ErrConflict) || len(again.EncodedResult) != 0 || third.calls.Load() != 1 {
		t.Fatal("original operations replayed", e)
	}
}

func TestModelEgressLocalRetryNativeBudgetExhaustionStopsNextCall(t *testing.T) {
	f := newEgressFixture(t, 1)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
	next := &nativeRetryAdapter{}
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, p, next)}
	out, e := retryNativeRun(t, f, b.store, steps, retryNativePolicy())
	if !errors.Is(e, modelegressbudget.ErrBudget) || len(out.EncodedResult) != 0 || first.calls.Load() != 1 || next.calls.Load() != 0 {
		t.Fatal("budget exhausted but adapter continued", e)
	}
	retryAssertOriginalBudgets(t, f, 1)
}

func retryAlternatePreview(t *testing.T, f *egressFixture, model string, approve bool) modelegressbudget.Preview {
	t.Helper()
	b := f.f.native.private.base
	price := f.price
	price.Version += "_alternate"
	price.Destination.Model = model
	if e := b.store.RegisterLocalModelPrice(b.ctx, price); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM model_budget_audit WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_egress_previews WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error(e)
			}
		}
		if _, e := b.pool.Exec(ctx, `DELETE FROM model_local_price_versions WHERE version=$1`, price.Version); e != nil {
			t.Error(e)
		}
	})
	p, e := b.store.PreviewOwnModelEgress(b.ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, PriceVersion: price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().UTC().Add(50 * time.Second).Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	if approve {
		if e = b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
			t.Fatal(e)
		}
	}
	return p
}

func TestModelEgressLocalRetryNativeFallbackHasItsOwnPriorApproval(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(map[bool]string{true: "approved", false: "unapproved"}[approved], func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", approved)
			if p.RequestDigest == alt.RequestDigest {
				t.Fatal("fixture does not have distinct price/digest")
			}
			first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "RATE_LIMIT"}}
			next := &nativeRetryAdapter{}
			next.model = "fallback"
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, alt, next)}
			out, e := retryNativeRun(t, f, b.store, steps, retryNativePolicy())
			if approved {
				if e != nil || first.calls.Load() != 1 || next.calls.Load() != 1 || len(out.EncodedResult) == 0 {
					t.Fatal("approved B not used", e)
				}
				retryAssertOriginalBudgets(t, f, 2)
			} else {
				if !errors.Is(e, modelegressbudget.ErrDenied) || first.calls.Load() != 0 || next.calls.Load() != 0 || len(out.EncodedResult) != 0 {
					t.Fatal("unapproved B allowed first call", e)
				}
				retryAssertOriginalBudgets(t, f, 0)
			}
		})
	}
}

type nativeRetryHookPort struct {
	modelegressbudget.LocalRetryPort
	afterDispatch   func()
	afterCheck      func()
	checks          atomic.Int32
	afterCapture    func()
	afterRevalidate func()
	revalidations   atomic.Int32
}

func (p *nativeRetryHookPort) CheckOwnLocalRetryDispatch(ctx context.Context, a agentevent.Access, proof modelegressbudget.LocalRetryProof, op string, req modelgateway.Request, dst modelegressbudget.LocalAttemptDestination, g *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := p.LocalRetryPort.CheckOwnLocalRetryDispatch(ctx, a, proof, op, req, dst, g)
	if e == nil && p.afterDispatch != nil {
		p.afterDispatch()
	}
	return cp, e
}

func TestModelEgressLocalRetryNativeGetterAfterDispatchCannotWithdrawBAndCallA(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	alt := retryAlternatePreview(t, f, "fallback", true)
	first := &nativeRetryAdapter{}
	second := &nativeRetryAdapter{}
	second.model = "fallback"
	var armed, revoked atomic.Bool
	port := &nativeRetryHookPort{LocalRetryPort: b.store, afterDispatch: func() { armed.Store(true) }}
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, first), retryNativeStep(t, f, port, alt, second)}
	first.onDestination = func() {
		if armed.Load() && revoked.CompareAndSwap(false, true) {
			if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, alt.ID); e != nil {
				t.Fatal(e)
			}
		}
	}
	out, e := retryNativeRun(t, f, port, steps, retryNativePolicy())
	if !revoked.Load() || !errors.Is(e, modelegressbudget.ErrDenied) || first.calls.Load() != 0 || second.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("getter after native dispatch handed over private query after original B revoke", e, first.calls.Load())
	}
}

func (p *nativeRetryHookPort) RevalidateOwnLocalRetryPlan(ctx context.Context, a agentevent.Access, proof modelegressbudget.LocalRetryProof, g *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := p.LocalRetryPort.RevalidateOwnLocalRetryPlan(ctx, a, proof, g)
	if e == nil && p.revalidations.Add(1) == 1 && p.afterRevalidate != nil {
		p.afterRevalidate()
	}
	return cp, e
}
func TestModelEgressLocalRetryNativeAllPlanMustReachActualDispatchFence(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	alt := retryAlternatePreview(t, f, "fallback", true)
	port := &nativeRetryHookPort{LocalRetryPort: b.store, afterRevalidate: func() {
		if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, alt.ID); e != nil {
			t.Fatal(e)
		}
	}}
	first := &nativeRetryAdapter{}
	second := &nativeRetryAdapter{}
	second.model = "fallback"
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, first), retryNativeStep(t, f, port, alt, second)}
	out, e := retryNativeRun(t, f, port, steps, retryNativePolicy())
	if !errors.Is(e, modelegressbudget.ErrDenied) || first.calls.Load() != 0 || second.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("outer full-plan read did not guard actual private query dispatch", e, first.calls.Load())
	}
}

func (p *nativeRetryHookPort) CaptureOwnLocalRetryPlan(ctx context.Context, a agentevent.Access, b []modelegressbudget.LocalRetryBinding, g *agentfeature.Controller) (modelegressbudget.LocalRetryProof, error) {
	proof, e := p.LocalRetryPort.CaptureOwnLocalRetryPlan(ctx, a, b, g)
	if e == nil && p.afterCapture != nil {
		p.afterCapture()
	}
	return proof, e
}

func TestModelEgressLocalRetryNativeSuccessMustRevalidateUnusedB(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	alt := retryAlternatePreview(t, f, "fallback", true)
	first := &nativeRetryAdapter{onCall: func() {
		if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, alt.ID); e != nil {
			t.Fatal(e)
		}
	}}
	second := &nativeRetryAdapter{}
	second.model = "fallback"
	steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, alt, second)}
	out, e := retryNativeRun(t, f, b.store, steps, retryNativePolicy())
	if !errors.Is(e, modelegressbudget.ErrDenied) || len(out.EncodedResult) != 0 || first.calls.Load() != 1 || second.calls.Load() != 0 {
		t.Fatal("successful A released after original B withdrawal", e, len(out.EncodedResult))
	}
}
func TestModelEgressLocalRetryNativeCaptureWaitCountsAgainstEntireRound(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	port := &nativeRetryHookPort{LocalRetryPort: b.store, afterCapture: func() { time.Sleep(200 * time.Millisecond) }}
	a := &nativeRetryAdapter{}
	policy := retryNativePolicy()
	policy.MaxElapsed = 150 * time.Millisecond
	policy.AttemptTimeout = 100 * time.Millisecond
	out, e := retryNativeRun(t, f, port, []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, a)}, policy)
	if e == nil || a.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("capture time was excluded from total deadline", e, a.calls.Load())
	}
}

func (p *nativeRetryHookPort) CheckOwnLocalRetryFailure(ctx context.Context, a agentevent.Access, proof modelegressbudget.LocalRetryProof, failure modelegressbudget.LocalRetryFailure, g *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := p.LocalRetryPort.CheckOwnLocalRetryFailure(ctx, a, proof, failure, g)
	if e == nil && p.checks.Add(1) == 1 && p.afterCheck != nil {
		p.afterCheck()
	}
	return cp, e
}
func TestModelEgressLocalRetryNativeBackoffSourceChangesRetireOriginalPlan(t *testing.T) {
	for _, kind := range []string{"revoke", "taskABA", "agentABA", "sessionExpiry", "gateABA"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			changed := make(chan struct{})
			port := &nativeRetryHookPort{LocalRetryPort: b.store}
			port.afterCheck = func() {
				go func() {
					defer close(changed)
					time.Sleep(40 * time.Millisecond)
					switch kind {
					case "revoke":
						if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); e != nil {
							t.Error(e)
						}
					case "taskABA":
						b.exec(`UPDATE agent_tasks SET query=query||' changed' WHERE id=$1`, p.TaskID)
						b.exec(`UPDATE agent_tasks SET query=$2 WHERE id=$1`, p.TaskID, p.Request.Messages[len(p.Request.Messages)-1].Content)
					case "agentABA":
						b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, p.Request.Agent.AgentID)
						b.exec(`UPDATE agents SET status='active' WHERE id=$1`, p.Request.Agent.AgentID)
					case "sessionExpiry":
						b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '40 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
						time.Sleep(55 * time.Millisecond)
					case "gateABA":
						if e := f.gate.Disable(agentfeature.Enrichment); e != nil {
							t.Error(e)
						}
						raw, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": map[string]bool{"agent_enrichment": true, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}, "pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
						cfg, e := agentfeature.ParseConfig(raw)
						if e != nil {
							t.Error(e)
							return
						}
						if e = f.gate.Replace(f.gate.Revision(), cfg); e != nil {
							t.Error(e)
						}
					}
				}()
			}
			first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
			next := &nativeRetryAdapter{}
			policy := retryNativePolicy()
			policy.BaseBackoff = 180 * time.Millisecond
			out, e := retryNativeRun(t, f, port, []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, first), retryNativeStep(t, f, port, p, next)}, policy)
			select {
			case <-changed:
			case <-time.After(3 * time.Second):
				t.Fatal("source change hook did not complete; native attempt stopped before backoff")
			}
			if e == nil || len(out.EncodedResult) != 0 || first.calls.Load() != 1 || next.calls.Load() != 0 {
				t.Fatal("backoff resumed stale plan", e)
			}
			if kind != "sessionExpiry" {
				retryAssertOriginalBudgets(t, f, 1)
			}
		})
	}
}

type nativeRetryErrorPort struct {
	modelegressbudget.LocalRetryPort
	point string
}

func (p *nativeRetryErrorPort) ReserveOwnModelAttempt(ctx context.Context, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller) (modelegressbudget.Reservation, error) {
	r, e := p.LocalRetryPort.ReserveOwnModelAttempt(ctx, a, in, c)
	if e == nil && p.point == "reserve" {
		return modelegressbudget.Reservation{}, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: true}
	}
	return r, e
}
func (p *nativeRetryErrorPort) BeginOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (modelgateway.Request, error) {
	r, e := p.LocalRetryPort.BeginOwnLocalModelAttempt(ctx, a, id, c)
	if e == nil && p.point == "begin" {
		return modelgateway.Request{}, modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}
	}
	return r, e
}
func (p *nativeRetryErrorPort) SettleOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, u modelegressbudget.LocalUsage) (modelegressbudget.Reservation, error) {
	r, e := p.LocalRetryPort.SettleOwnLocalModelAttempt(ctx, a, id, u)
	if e == nil && p.point == "settle" {
		return modelegressbudget.Reservation{}, modelgateway.ProviderError{Code: "TEMPORARY", Retryable: true}
	}
	return r, e
}
func TestModelEgressLocalRetryNativePortProviderErrorCannotSignRetry(t *testing.T) {
	for _, point := range []string{"reserve", "begin", "settle"} {
		t.Run(point, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			port := &nativeRetryErrorPort{LocalRetryPort: b.store, point: point}
			first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "TEMPORARY"}}
			next := &nativeRetryAdapter{}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, port, p, first), retryNativeStep(t, f, port, p, next)}
			out, e := retryNativeRun(t, f, port, steps, retryNativePolicy())
			if e == nil || next.calls.Load() != 0 || len(out.EncodedResult) != 0 {
				t.Fatal("port error granted retry", e)
			}
			want := int32(0)
			if point == "settle" {
				want = 1
			}
			if first.calls.Load() != want {
				t.Fatal(first.calls.Load())
			}
			retryAssertOriginalBudgets(t, f, 1)
			if _, e = b.store.ReadOwnLocalModelAttempt(b.ctx, f.f.native.access, steps[0].Input.OperationID); e != nil {
				t.Fatal("original operation unrecoverable", e)
			}
		})
	}
}

func TestModelEgressLocalRetryNativeLateOrCancelledProviderResponseDoesNotContinue(t *testing.T) {
	for _, kind := range []string{"timeout", "cancel", "unknown", "refused", "invalidHint"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			first := &nativeRetryAdapter{failure: modelgateway.ProviderError{Code: "RATE_LIMIT"}}
			next := &nativeRetryAdapter{}
			policy := retryNativePolicy()
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			switch kind {
			case "timeout":
				policy.AttemptTimeout = 200 * time.Millisecond
				first.onCall = func() { time.Sleep(240 * time.Millisecond) }
			case "cancel":
				first.onCall = cancel
			case "unknown":
				first.failure = errors.New("unknown local delivery")
			case "refused":
				first.failure = modelgateway.ProviderError{Code: "REFUSED"}
			case "invalidHint":
				first.failure = modelgateway.NewRetryAfterError(modelgateway.ProviderError{Code: "RATE_LIMIT"}, -1)
			}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, p, next)}
			runner := modelegressbudget.NewLocalRetryRunner(b.store, f.gate, policy)
			out, e := runner.Run(ctx, f.f.native.access, steps)
			if e == nil || first.calls.Load() != 1 || next.calls.Load() != 0 || len(out.EncodedResult) != 0 {
				t.Fatal("terminal/lost response continued", e, first.calls.Load())
			}
			retryAssertOriginalBudgets(t, f, 1)
		})
	}
}
