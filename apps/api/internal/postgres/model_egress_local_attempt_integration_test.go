package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func nativeAttemptEncoded(t *testing.T, r modelgateway.Request) []byte {
	t.Helper()
	a := &nativeLocalAttemptAdapter{}
	h, e := modelgateway.NewOfflineHarness(a)
	if e != nil {
		t.Fatal(e)
	}
	result, e := h.Complete(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	return encoded
}

func TestModelEgressLocalAttemptNativeConcurrentSameOperationOneDispatch(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	a := &nativeLocalAttemptAdapter{}
	d := nativeAttemptDriver(t, b.store, f.gate, a)
	var wg sync.WaitGroup
	success := atomic.Int32{}
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			o, e := d.Once(b.ctx, f.f.native.access, in)
			if e == nil {
				if len(o.EncodedResult) == 0 {
					t.Error("empty confirmed result")
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
		t.Fatal("concurrent dispatch duplicated", success.Load(), a.calls.Load())
	}
	rows, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range rows {
		if r.Allocated.Requests != 1 {
			t.Fatal("duplicate reservation", r)
		}
	}
}

func TestModelEgressLocalAttemptNativeFinalReleaseWaitUsesCurrentAuthority(t *testing.T) {
	for _, kind := range []string{"deadline", "session", "revoke", "agentABA", "queryABA", "kill", "context"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			p := f.preview(t, f.f.native.task.ID, true)
			if kind == "deadline" {
				var e error
				p, e = b.store.PreviewOwnModelEgress(ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: p.TaskID, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().Add(1400 * time.Millisecond).UTC().Truncate(time.Microsecond)})
				if e != nil {
					t.Fatal(e)
				}
				if e = b.store.ApproveOwnModelEgress(ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
					t.Fatal(e)
				}
			}
			in := nativeAttemptInput(t, f, p)
			captured := &nativeCaptureAttemptPort{LocalAttemptPort: b.store}
			d := nativeAttemptDriver(t, captured, f.gate, &nativeLocalAttemptAdapter{})
			if _, e := d.Once(ctx, f.f.native.access, in); e != nil {
				t.Fatal(e)
			}
			req := captured.request
			buffer := captured.buffer

			cutoff := p.ExpiresAt
			if kind == "session" {
				cutoff = time.Now().Add(850 * time.Millisecond)
				b.exec(`UPDATE sessions SET idle_expires_at=$2 WHERE id=$1`, f.f.native.private.ownerSession, cutoff)
			}
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			app := "air011_release_" + strings.ReplaceAll(in.OperationID, "-", "")
			cfg.ConnConfig.RuntimeParams["application_name"] = app
			cfg.MaxConns = 1
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			blocker, e := b.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			if e = egressOwnerLock(ctx, blocker, b.person.ID); e != nil {
				t.Fatal(e)
			}
			var blockerPID int
			if e = blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); e != nil {
				t.Fatal(e)
			}
			releaseCtx, releaseCancel := context.WithCancel(ctx)
			defer releaseCancel()
			done := make(chan error, 1)
			go func() {
				_, err := store.ReleaseOwnLocalModelAttempt(releaseCtx, f.f.native.access, in.OperationID, f.gate, req, buffer)
				done <- err
			}()
			for {
				var waiting bool
				if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND $2=ANY(pg_blocking_pids(pid)))`, app, blockerPID).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				select {
				case e := <-done:
					t.Fatal("did not wait on exact native owner lock", e)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(8 * time.Millisecond):
				}
			}
			switch kind {
			case "deadline", "session":
				for time.Now().Before(cutoff.Add(25 * time.Millisecond)) {
					time.Sleep(8 * time.Millisecond)
				}
			case "revoke":
				b.exec(`UPDATE model_egress_previews SET status='REVOKED',revoked_at=clock_timestamp(),revision=revision+1 WHERE id=$1`, p.ID)
			case "agentABA":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, p.Request.Agent.AgentID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, p.Request.Agent.AgentID)
			case "queryABA":
				b.exec(`UPDATE agent_tasks SET query=query||' changed',updated_at=clock_timestamp() WHERE id=$1`, p.TaskID)
				b.exec(`UPDATE agent_tasks SET query=$2,updated_at=clock_timestamp() WHERE id=$1`, p.TaskID, req.Messages[len(req.Messages)-1].Content)
			case "kill":
				if e = f.gate.Disable(agentfeature.Enrichment); e != nil {
					t.Fatal(e)
				}
			case "context":
				releaseCancel()
			}
			if e = blocker.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			e = <-done
			switch kind {
			case "kill":
				if !errors.Is(e, modelegressbudget.ErrUnavailable) {
					t.Fatal(e)
				}
			case "context":
				if !errors.Is(e, context.Canceled) {
					t.Fatal(e)
				}
			default:
				if !errors.Is(e, modelegressbudget.ErrDenied) {
					t.Fatal("final source permission wrong", e)
				}
			}
			var state string
			if e = b.pool.QueryRow(ctx, `SELECT state FROM model_budget_reservations WHERE operation_id=$1`, in.OperationID).Scan(&state); e != nil || state != "SETTLED" {
				t.Fatal("release altered accounting", state, e)
			}
		})
	}
}

type localAttemptRecoveryProcessInput struct {
	Digest                [32]byte
	Operation, Root, Task string
}

// Only an explicitly spawned local test subprocess enters this branch.
func TestModelEgressLocalAttemptNativeProcessRecoveryChild(t *testing.T) {
	if os.Getenv("BIRDTIE_LOCAL_ATTEMPT_RECOVERY_CHILD") != "1" {
		return
	}
	var in localAttemptRecoveryProcessInput
	if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
		t.Fatal("private child input unavailable")
	}
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	defer c()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal("owned database connection unavailable")
	}
	defer pool.Close()
	s := New(pool, false)
	control, e := s.ReadOwnLocalModelAttempt(ctx, agentevent.Access{SessionDigest: in.Digest}, in.Operation)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := s.ReadOwnModelBudget(ctx, agentevent.Access{SessionDigest: in.Digest}, in.Root, in.Task)
	if e != nil || len(rows) != 4 {
		t.Fatal("accounting recovery failed", e)
	}
	for _, r := range rows {
		if r.Allocated.Requests != 1 {
			t.Fatal("child allocated new attempt")
		}
	}
	body, e := json.Marshal(control)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(os.Getenv("BIRDTIE_LOCAL_ATTEMPT_RECOVERY_OUTPUT"), body, 0600); e != nil {
		t.Fatal("owned result write failed")
	}
	t.Logf("LOCAL_SYNTHETIC recovery child PID=%d state=%s; no dispatch", os.Getpid(), control.State)
}
func TestModelEgressLocalAttemptNativeOSRestartRecoversOriginalAccountingOnly(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	a := &nativeLocalAttemptAdapter{}
	d := nativeAttemptDriver(t, b.store, f.gate, a)
	if _, e := d.Once(b.ctx, f.f.native.access, in); e != nil {
		t.Fatal(e)
	}
	var before string
	if e := b.pool.QueryRow(b.ctx, `SELECT row_to_json(r)::text||':'||r.xmin::text FROM model_budget_reservations r WHERE operation_id=$1`, in.OperationID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	secretInput, e := json.Marshal(localAttemptRecoveryProcessInput{Digest: f.f.native.access.SessionDigest, Operation: in.OperationID, Root: f.root, Task: p.TaskID})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		output := t.TempDir() + "/receipt.json"
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestModelEgressLocalAttemptNativeProcessRecoveryChild$", "-test.v")
		cmd.Env = append(os.Environ(), "BIRDTIE_LOCAL_ATTEMPT_RECOVERY_CHILD=1", "BIRDTIE_LOCAL_ATTEMPT_RECOVERY_OUTPUT="+output)
		cmd.Stdin = strings.NewReader(string(secretInput))
		raw, e := cmd.CombinedOutput()
		c()
		if e != nil {
			t.Fatal("actual subprocess failed", e, string(raw))
		}
		t.Log(string(raw))
		wire, e := os.ReadFile(output)
		if e != nil {
			t.Fatal(e)
		}
		var control modelegressbudget.AttemptControl
		if json.Unmarshal(wire, &control) != nil || control.OperationID != in.OperationID || control.State != "SETTLED" {
			t.Fatal("wrong original operation receipt")
		}
	}
	var after string
	if e = b.pool.QueryRow(b.ctx, `SELECT row_to_json(r)::text||':'||r.xmin::text FROM model_budget_reservations r WHERE operation_id=$1`, in.OperationID).Scan(&after); e != nil || after != before || a.calls.Load() != 1 {
		t.Fatal("process recovery mutated ledger or dispatched", e)
	}
}

type nativeLocalAttemptAdapter struct {
	calls               atomic.Int32
	after               func()
	unknown             bool
	raw                 []byte
	model               string
	destinationOverride *modelegressbudget.LocalAttemptDestination
}

type nativeCaptureAttemptPort struct {
	modelegressbudget.LocalAttemptPort
	request        modelgateway.Request
	buffer         modelegressbudget.LocalResultBuffer
	differentUsage bool
	afterRelease   func(modelegressbudget.LocalReleaseCheckpoint)
	afterDispatch  func(modelegressbudget.LocalReleaseCheckpoint)
}

func (p *nativeCaptureAttemptPort) ReleaseOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller, r modelgateway.Request, b modelegressbudget.LocalResultBuffer) (modelegressbudget.LocalReleaseCheckpoint, error) {
	p.request = r
	p.buffer = b
	cp, err := p.LocalAttemptPort.ReleaseOwnLocalModelAttempt(ctx, a, id, c, r, b)
	if err == nil && p.afterRelease != nil {
		p.afterRelease(cp)
	}
	return cp, err
}
func (p *nativeCaptureAttemptPort) SettleOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, u modelegressbudget.LocalUsage) (modelegressbudget.Reservation, error) {
	if p.differentUsage {
		i, o := int64(2), int64(4)
		var e error
		u, e = modelegressbudget.NewLocalUsage(modelgateway.Usage{Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &i, OutputTokens: &o})
		if e != nil {
			return modelegressbudget.Reservation{}, e
		}
	}
	return p.LocalAttemptPort.SettleOwnLocalModelAttempt(ctx, a, id, u)
}
func TestModelEgressLocalAttemptNativeForgedUsageCannotRelease(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	port := &nativeCaptureAttemptPort{LocalAttemptPort: b.store, differentUsage: true}
	a := &nativeLocalAttemptAdapter{}
	d := nativeAttemptDriver(t, port, f.gate, a)
	out, e := d.Once(b.ctx, f.f.native.access, in)
	if !errors.Is(e, modelegressbudget.ErrDenied) || len(out.EncodedResult) != 0 || a.calls.Load() != 1 {
		t.Fatal("normalized encoded usage disagrees with actual native accounting", e)
	}
	if _, e := json.Marshal(port.buffer); !errors.Is(e, modelegressbudget.ErrServerOnly) {
		t.Fatal("buffer transferable", e)
	}
	var fake modelegressbudget.LocalResultBuffer
	if e := json.Unmarshal([]byte(`{"operation":"original","encoded":"private"}`), &fake); !errors.Is(e, modelegressbudget.ErrServerOnly) {
		t.Fatal(e)
	}
	if _, e = b.store.ReleaseOwnLocalModelAttempt(b.ctx, f.f.native.access, in.OperationID, f.gate, port.request, fake); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("JSON forged buffer accepted", e)
	}
}

func (a *nativeLocalAttemptAdapter) Descriptor() modelgateway.ProviderDescriptor {
	model := "local"
	if a.model != "" {
		model = a.model
	}
	return modelgateway.ProviderDescriptor{ProviderID: "fake", ModelID: model, ModelVersion: "v1", Mode: modelgateway.OfflineContract}
}

func TestModelEgressLocalAttemptNativeOffCrossOwnerAndDifferentRequestNeverRelease(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	a := &nativeLocalAttemptAdapter{}
	off := nativeAttemptDriver(t, b.store, egressGate(t, false), a)
	if o, e := off.Once(b.ctx, f.f.native.access, in); !errors.Is(e, modelegressbudget.ErrUnavailable) || len(o.EncodedResult) != 0 || a.calls.Load() != 0 {
		t.Fatal("OFF dispatch", e)
	}
	var n int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE operation_id=$1`, in.OperationID).Scan(&n); e != nil || n != 0 {
		t.Fatal("OFF retained operation", e)
	}
	cap := &nativeCaptureAttemptPort{LocalAttemptPort: b.store}
	d := nativeAttemptDriver(t, cap, f.gate, a)
	if _, e := d.Once(b.ctx, f.f.native.access, in); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Recover(b.ctx, agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}, in.OperationID); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("cross-owner recovery", e)
	}
	for _, kind := range []string{"operation", "request", "agent", "run"} {
		t.Run(kind, func(t *testing.T) {
			r := cap.request
			op := in.OperationID
			switch kind {
			case "operation":
				op = egressID(t, f)
			case "request":
				r.Messages = append([]modelgateway.Message{}, r.Messages...)
				r.Messages[len(r.Messages)-1].Content = "不同私人查询"
			case "agent":
				r.Agent.AgentID = egressID(t, f)
			case "run":
				r.RunID = egressID(t, f)
			}
			if _, e := b.store.ReleaseOwnLocalModelAttempt(b.ctx, f.f.native.access, op, f.gate, r, cap.buffer); !errors.Is(e, modelegressbudget.ErrDenied) {
				t.Fatal("other operation/request reused buffer", e)
			}
		})
	}
	bad := &nativeLocalAttemptAdapter{model: "different"}
	out, e := nativeAttemptDriver(t, b.store, f.gate, bad).Once(b.ctx, f.f.native.access, nativeAttemptInput(t, f, p))
	if !errors.Is(e, modelegressbudget.ErrDenied) || len(out.EncodedResult) != 0 || bad.calls.Load() != 0 {
		t.Fatal("unapproved synthetic destination released", e)
	}
}

func TestModelEgressLocalAttemptNativePoolWaitBeforeSourceCaptureUsesOriginalApproval(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	cap := &nativeCaptureAttemptPort{LocalAttemptPort: b.store}
	d := nativeAttemptDriver(t, cap, f.gate, &nativeLocalAttemptAdapter{})
	if _, e := d.Once(b.ctx, f.f.native.access, in); e != nil {
		t.Fatal(e)
	}
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	defer c()
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
	hold, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Release()
	beforeWait := pool.Stat().EmptyAcquireCount()
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := New(pool, false).ReleaseOwnLocalModelAttempt(ctx, f.f.native.access, in.OperationID, f.gate, cap.request, cap.buffer)
		done <- err
	}()
	<-started
	if pool.Stat().AcquiredConns() != 1 {
		t.Fatal("exclusive pool not held")
	}
	select {
	case e := <-done:
		t.Fatal("expected blocked pool acquisition", e)
	case <-time.After(20 * time.Millisecond):
	}
	// The real original owner revoke can commit while the release has no connection.
	if e = b.store.RevokeOwnModelEgress(ctx, f.f.native.access, p.ID); e != nil {
		t.Fatal(e)
	}
	hold.Release()
	if e = <-done; !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("revoked original preview after actual pool wait", e)
	}
	if pool.Stat().EmptyAcquireCount() <= beforeWait {
		t.Fatal("actual pool wait not counted")
	}
}
func (a *nativeLocalAttemptAdapter) Complete(context.Context, modelgateway.ProviderRequest) ([]byte, error) {
	a.calls.Add(1)
	if a.raw != nil {
		return a.raw, nil
	}
	if a.after != nil {
		a.after()
	}
	usage := `{"status":"KNOWN","input_tokens":2,"output_tokens":3}`
	if a.unknown {
		usage = `{"status":"UNKNOWN"}`
	}
	return []byte(`{"status":"COMPLETED","request_id":"synthetic-local-only","finish_reason":"stop","structured":{"answer":"本地合成答案，不是生产模型结果。","entity_refs":[]},"usage":` + usage + `}`), nil
}

func TestModelEgressLocalAttemptNativePostCommitSessionDeadlineStopsEncodedResult(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
	port := &nativeCaptureAttemptPort{LocalAttemptPort: b.store, afterRelease: func(cp modelegressbudget.LocalReleaseCheckpoint) {
		if !cp.ValidUntil().Before(p.ExpiresAt) {
			t.Error("Session idle not included in earliest checkpoint")
		}
		time.Sleep(cp.Remaining(time.Now()) + 35*time.Millisecond)
	}}
	a := &nativeLocalAttemptAdapter{}
	d := nativeAttemptDriver(t, port, f.gate, a)
	out, e := d.Once(b.ctx, f.f.native.access, in)
	if !errors.Is(e, modelegressbudget.ErrDenied) || len(out.EncodedResult) != 0 || a.calls.Load() != 1 {
		t.Fatal("delayed actual native commit return released past Session idle", e)
	}
}

type nativeAttemptFaultPort struct {
	modelegressbudget.LocalAttemptPort
	point      string
	afterBegin func()
}

func (p *nativeAttemptFaultPort) ReserveOwnModelAttempt(ctx context.Context, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller) (modelegressbudget.Reservation, error) {
	r, e := p.LocalAttemptPort.ReserveOwnModelAttempt(ctx, a, in, c)
	if e == nil && p.point == "reserveCommitted" {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
	}
	return r, e
}
func (p *nativeAttemptFaultPort) BeginOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (modelgateway.Request, error) {
	r, e := p.LocalAttemptPort.BeginOwnLocalModelAttempt(ctx, a, id, c)
	if e == nil && p.afterBegin != nil {
		p.afterBegin()
	}
	return r, e
}
func (p *nativeAttemptFaultPort) SettleOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, u modelegressbudget.LocalUsage) (modelegressbudget.Reservation, error) {
	if p.point == "settleNotCommitted" {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
	}
	r, e := p.LocalAttemptPort.SettleOwnLocalModelAttempt(ctx, a, id, u)
	if e == nil && p.point == "settleCommitted" {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
	}
	return r, e
}
func TestModelEgressLocalAttemptNativeUnknownReserveAndSettleOriginalIDOnly(t *testing.T) {
	for _, point := range []string{"reserveCommitted", "settleNotCommitted", "settleCommitted"} {
		t.Run(point, func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{}
			d := nativeAttemptDriver(t, &nativeAttemptFaultPort{LocalAttemptPort: b.store, point: point}, f.gate, a)
			out, e := d.Once(b.ctx, f.f.native.access, in)
			if !errors.Is(e, modelegressbudget.ErrUnavailable) || len(out.EncodedResult) != 0 {
				t.Fatal(e)
			}
			r, e := d.Recover(b.ctx, f.f.native.access, in.OperationID)
			want := "RESERVED"
			calls := int32(0)
			if point == "settleNotCommitted" {
				want = "IN_FLIGHT"
				calls = 1
			}
			if point == "settleCommitted" {
				want = "SETTLED"
				calls = 1
			}
			if e != nil || r.State != want || a.calls.Load() != calls {
				t.Fatal(r, e, a.calls.Load())
			}
			var n int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&n); e != nil || n != 1 {
				t.Fatal("unknown attempted new ID", e, n)
			}
		})
	}
}

func TestModelEgressLocalAttemptNativeBeginThenGateOrContextStopsCall(t *testing.T) {
	for _, kind := range []string{"gateABA", "context", "deadline", "revoke", "query", "descriptorChanged"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			if kind == "deadline" {
				var e error
				p, e = b.store.PreviewOwnModelEgress(b.ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: p.TaskID, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().Add(850 * time.Millisecond).UTC().Truncate(time.Microsecond)})
				if e != nil {
					t.Fatal(e)
				}
				if e = b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
					t.Fatal(e)
				}
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{}
			hook := func() {
				switch kind {
				case "revoke":
					if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); e != nil {
						t.Fatal(e)
					}
				case "query":
					b.exec(`UPDATE agent_tasks SET query=query||' changed before adapter' WHERE id=$1`, p.TaskID)
				case "descriptorChanged":
					a.model = "different"
				case "context":
					cancel()
				case "deadline":
					time.Sleep(time.Until(p.ExpiresAt) + 25*time.Millisecond)
				case "gateABA":
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
			}
			d := nativeAttemptDriver(t, &nativeAttemptFaultPort{LocalAttemptPort: b.store, afterBegin: hook}, f.gate, a)
			out, e := d.Once(ctx, f.f.native.access, in)
			if e == nil || len(out.EncodedResult) != 0 || a.calls.Load() != 0 {
				t.Fatal("Begin followed by invalid barrier dispatched", e)
			}
			r, err := b.store.ReadOwnLocalModelAttempt(b.ctx, f.f.native.access, in.OperationID)
			if err != nil || (r.State != "IN_FLIGHT" && r.State != "UNKNOWN") {
				t.Fatal(r, err)
			}
		})
	}
}

func TestModelEgressLocalAttemptNativeMalformedUsageNeverRefundsOrReleases(t *testing.T) {
	for _, raw := range []string{`{"status":"COMPLETED","request_id":"fake-local","finish_reason":"stop","structured":{"answer":"合成","entity_refs":[]},"usage":{"status":"KNOWN","input_tokens":2,"output_tokens":999}}`, `{"status":"COMPLETED","request_id":"fake-local","finish_reason":"stop","structured":{"answer":"合成","entity_refs":[]},"usage":{"status":"KNOWN","input_tokens":101,"output_tokens":3}}`} {
		t.Run(map[bool]string{true: "outputOverBound", false: "inputOverBound"}[strings.Contains(raw, "999")], func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{raw: []byte(raw)}
			d := nativeAttemptDriver(t, b.store, f.gate, a)
			out, e := d.Once(b.ctx, f.f.native.access, in)
			if e == nil || len(out.EncodedResult) != 0 || a.calls.Load() != 1 {
				t.Fatal("invalid usage released", e)
			}
			rows, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
			if e != nil {
				t.Fatal(e)
			}
			bound, e := modelegressbudget.Bound(f.price, 64)
			if e != nil {
				t.Fatal(e)
			}
			for _, r := range rows {
				if r.Allocated.Requests != 1 || r.Allocated.InputTokens != bound.InputTokens || r.Allocated.OutputTokens != bound.OutputTokens || r.Allocated.CostMicros != bound.CostMicros {
					t.Fatal("invalid usage refunded or altered conservative hold", r)
				}
			}
		})
	}
}

func TestModelEgressLocalAttemptNativeChildTaskSharesRootAndIdleRefreshPositive(t *testing.T) {
	f := newEgressFixture(t, 1)
	b := f.f.native.private.base
	child, e := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: "合成下级Task", Intent: "PENDING", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if e != nil {
		t.Fatal(e)
	}
	binding := egressID(t, f)
	req := f.f.request(t, 0, binding)
	if _, e = b.store.BindModelTaskConfiguration(b.ctx, f.f.native.access, child.ID, f.f.route.Version, f.f.route.Revision, req); e != nil {
		t.Fatal(e)
	}
	if e = b.store.BindOwnModelBudgetTask(b.ctx, f.f.native.access, modelegressbudget.TaskInput{RootTraceID: f.root, TaskID: child.ID, BindingID: binding, Limits: f.limits}); e != nil {
		t.Fatal(e)
	}
	p := f.preview(t, f.f.native.task.ID, true)
	p2 := f.preview(t, child.ID, true)
	if _, e = b.store.Authenticate(b.ctx, f.f.native.access.SessionDigest); e != nil {
		t.Fatal(e)
	}
	a := &nativeLocalAttemptAdapter{}
	d := nativeAttemptDriver(t, b.store, f.gate, a)
	if _, e = d.Once(b.ctx, f.f.native.access, nativeAttemptInput(t, f, p)); e != nil {
		t.Fatal("normal idle refresh invalidated exact approved scope", e)
	}
	out, e := d.Once(b.ctx, f.f.native.access, nativeAttemptInput(t, f, p2))
	if !errors.Is(e, modelegressbudget.ErrBudget) || len(out.EncodedResult) != 0 || a.calls.Load() != 1 {
		t.Fatal("child bypassed root ceiling", e)
	}
}
func nativeAttemptInput(t *testing.T, f *egressFixture, p modelegressbudget.Preview) modelegressbudget.ReserveInput {
	return modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}
}
func nativeAttemptDriver(t *testing.T, port modelegressbudget.LocalAttemptPort, g *agentfeature.Controller, a *nativeLocalAttemptAdapter) *modelegressbudget.LocalAttemptDriver {
	t.Helper()
	d := modelegressbudget.NewLocalAttemptDriver(port, a, g)
	if d == nil {
		t.Fatal("local adapter construction denied")
	}
	return d
}

func TestModelEgressLocalAttemptNativeOnceAndRecovery(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "known", true: "unknownUsage"}[unknown], func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{unknown: unknown}
			d := nativeAttemptDriver(t, b.store, f.gate, a)
			out, e := d.Once(b.ctx, f.f.native.access, in)
			if e != nil || len(out.EncodedResult) == 0 || a.calls.Load() != 1 {
				t.Fatal("native local once", e, a.calls.Load(), string(out.EncodedResult))
			}
			want := "SETTLED"
			if unknown {
				want = "UNKNOWN"
			}
			if out.Control.State != want {
				t.Fatal(out.Control)
			}
			r, e := d.Recover(b.ctx, f.f.native.access, in.OperationID)
			if e != nil || r.State != want || r.ModelAccess != "UNAVAILABLE" {
				t.Fatal(r, e)
			}
			wire, _ := json.Marshal(r)
			if string(wire) == "" {
				t.Fatal("missing control")
			}
			out, e = d.Once(b.ctx, f.f.native.access, in)
			if !errors.Is(e, modelegressbudget.ErrConflict) || len(out.EncodedResult) != 0 || a.calls.Load() != 1 {
				t.Fatal("same settled operation replayed", e, a.calls.Load())
			}
			rows, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
			if e != nil || len(rows) != 4 {
				t.Fatal(rows, e)
			}
			for _, v := range rows {
				if v.Allocated.Requests != 1 {
					t.Fatal("duplicate charged", v)
				}
			}
			other := f.f.native.access
			other.SessionDigest = [32]byte{}
			if _, e = d.Recover(b.ctx, other, in.OperationID); !errors.Is(e, modelegressbudget.ErrDenied) {
				t.Fatal("anonymous recovery", e)
			}
		})
	}
}
func TestModelEgressLocalAttemptNativeAccountingCannotReleaseWithdrawnAnswer(t *testing.T) {
	for _, kind := range []string{"revoke", "query", "agentABA", "gate"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{after: func() {
				switch kind {
				case "revoke":
					if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); e != nil {
						t.Fatal(e)
					}
				case "query":
					b.exec(`UPDATE agent_tasks SET query=query||' changed',updated_at=clock_timestamp() WHERE id=$1`, p.TaskID)
				case "agentABA":
					b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, p.Request.Agent.AgentID)
					b.exec(`UPDATE agents SET status='active' WHERE id=$1`, p.Request.Agent.AgentID)
				case "gate":
					if e := f.gate.Disable(agentfeature.Enrichment); e != nil {
						t.Fatal(e)
					}
				}
			}}
			d := nativeAttemptDriver(t, b.store, f.gate, a)
			out, e := d.Once(b.ctx, f.f.native.access, in)
			if kind == "gate" {
				if !errors.Is(e, modelegressbudget.ErrUnavailable) {
					t.Fatal(e)
				}
			} else if !errors.Is(e, modelegressbudget.ErrDenied) {
				t.Fatal("withdrawn source error", e)
			}
			if len(out.EncodedResult) != 0 || a.calls.Load() != 1 {
				t.Fatal("withdrawn answer released")
			}
			r, e := d.Recover(b.ctx, f.f.native.access, in.OperationID)
			if e != nil || r.State != "SETTLED" {
				t.Fatal("accounting must remain recoverable without result", r, e)
			}
		})
	}
}

type nativeUnknownAttemptPort struct {
	modelegressbudget.LocalAttemptPort
	beforeBegin, afterBegin bool
}

func (p *nativeUnknownAttemptPort) BeginOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (modelgateway.Request, error) {
	if p.beforeBegin {
		return modelgateway.Request{}, modelegressbudget.ErrUnavailable
	}
	r, e := p.LocalAttemptPort.BeginOwnLocalModelAttempt(ctx, a, id, c)
	if e == nil && p.afterBegin {
		return modelgateway.Request{}, modelegressbudget.ErrUnavailable
	}
	return r, e
}
func TestModelEgressLocalAttemptNativeUnknownBeginStopsDispatch(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "notCommitted", true: "committedReceiptLost"}[committed], func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{}
			d := nativeAttemptDriver(t, &nativeUnknownAttemptPort{LocalAttemptPort: b.store, beforeBegin: !committed, afterBegin: committed}, f.gate, a)
			out, e := d.Once(b.ctx, f.f.native.access, in)
			if !errors.Is(e, modelegressbudget.ErrUnavailable) || len(out.EncodedResult) != 0 || a.calls.Load() != 0 {
				t.Fatal("unknown Begin dispatched", e)
			}
			r, e := d.Recover(b.ctx, f.f.native.access, in.OperationID)
			want := "RESERVED"
			if committed {
				want = "IN_FLIGHT"
			}
			if e != nil || r.State != want {
				t.Fatal(r, e)
			}
			if committed {
				if _, e = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, in.OperationID); !errors.Is(e, modelegressbudget.ErrConflict) {
					t.Fatal("in-flight claimed unsent", e)
				}
			} else {
				if _, e = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, in.OperationID); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func (a *nativeLocalAttemptAdapter) LocalDestination() modelegressbudget.LocalAttemptDestination {
	if a.destinationOverride != nil {
		return *a.destinationOverride
	}
	d := a.Descriptor()
	return modelegressbudget.LocalAttemptDestination{Key: modelcapability.Key{Provider: d.ProviderID, Model: d.ModelID, Version: d.ModelVersion, WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: modelegressbudget.Retention}
}

func (p *nativeCaptureAttemptPort) CheckOwnLocalModelAttemptDispatch(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller, r modelgateway.Request, d modelegressbudget.LocalAttemptDestination) (modelegressbudget.LocalReleaseCheckpoint, error) {
	cp, e := p.LocalAttemptPort.CheckOwnLocalModelAttemptDispatch(ctx, a, id, c, r, d)
	if e == nil && p.afterDispatch != nil {
		p.afterDispatch(cp)
	}
	return cp, e
}
func TestModelEgressLocalAttemptNativeDispatchCheckpointExpiresBeforeAdapterCall(t *testing.T) {
	f := newEgressFixture(t, 2)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := nativeAttemptInput(t, f, p)
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
	port := &nativeCaptureAttemptPort{LocalAttemptPort: b.store, afterDispatch: func(cp modelegressbudget.LocalReleaseCheckpoint) {
		if !cp.ValidUntil().Before(p.ExpiresAt) {
			t.Error("dispatch checkpoint lacks shorter Session idle expiry")
		}
		time.Sleep(cp.Remaining(time.Now()) + 35*time.Millisecond)
	}}
	a := &nativeLocalAttemptAdapter{}
	out, e := nativeAttemptDriver(t, port, f.gate, a).Once(b.ctx, f.f.native.access, in)
	if !errors.Is(e, modelegressbudget.ErrDenied) || a.calls.Load() != 0 || len(out.EncodedResult) != 0 {
		t.Fatal("expired original dispatch checkpoint passed query", e, a.calls.Load())
	}
}
func TestModelEgressLocalAttemptNativeExactWireRegionRetentionBeforeDispatch(t *testing.T) {
	for _, field := range []string{"wire", "region", "retention"} {
		t.Run(field, func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			a := &nativeLocalAttemptAdapter{}
			d := a.LocalDestination()
			switch field {
			case "wire":
				d.Key.WireContract = "different.v1"
			case "region":
				d.Region = modelcapability.EU
			case "retention":
				d.Retention = "different_retention"
			}
			a.destinationOverride = &d
			out, e := nativeAttemptDriver(t, b.store, f.gate, a).Once(b.ctx, f.f.native.access, nativeAttemptInput(t, f, p))
			if !errors.Is(e, modelegressbudget.ErrDenied) || a.calls.Load() != 0 || len(out.EncodedResult) != 0 {
				t.Fatal("mismatched local destination received query", e, a.calls.Load())
			}
		})
	}
}

func TestModelEgressLocalAttemptNativeAdapterChangesDuringCallDiscardsRaw(t *testing.T) {
	for _, kind := range []string{"descriptor", "destination"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 2)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			in := nativeAttemptInput(t, f, p)
			a := &nativeLocalAttemptAdapter{}
			a.after = func() {
				if kind == "descriptor" {
					a.model = "changed"
				} else {
					d := a.LocalDestination()
					d.Region = modelcapability.EU
					a.destinationOverride = &d
				}
			}
			out, e := nativeAttemptDriver(t, b.store, f.gate, a).Once(b.ctx, f.f.native.access, in)
			if e == nil || a.calls.Load() != 1 || len(out.EncodedResult) != 0 {
				t.Fatal("changed adapter raw released", e, a.calls.Load())
			}
			state, e := b.store.ReadOwnLocalModelAttempt(b.ctx, f.f.native.access, in.OperationID)
			if e != nil || state.State != "UNKNOWN" {
				t.Fatal("failed dispatch did not keep original conservative hold", state, e)
			}
		})
	}
}
