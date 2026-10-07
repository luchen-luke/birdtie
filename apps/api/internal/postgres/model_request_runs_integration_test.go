package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func modelRunID(t *testing.T, f *egressFixture) string {
	t.Helper()
	id := egressID(t, f)
	b := f.f.native.private.base
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if _, e := b.pool.Exec(ctx, `DELETE FROM model_request_runs WHERE id=$1 AND owner_id=$2`, id, b.person.ID); e != nil {
			t.Error("owned model run cleanup", e)
		}
		var n int
		if e := b.pool.QueryRow(ctx, `SELECT count(*) FROM model_request_runs WHERE id=$1`, id).Scan(&n); e != nil || n != 0 {
			t.Error("model run residue", e, n)
		}
	})
	return id
}

func TestModelRequestRunNativeDownWaitsForCreateAndRetainsCommittedHistory(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	id := modelRunID(t, f)
	bindings := modelRunBindings(t, f, p, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "model-run-down:" + id
	name := "model_run_down_" + strings.ReplaceAll(id, "-", "")
	hold, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); e != nil {
		t.Fatal(e)
	}
	var holderPID int
	if e = hold.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); e != nil {
		t.Fatal(e)
	}
	// A per-owned-run last-step hook holds the real Create transaction after both
	// metadata relations have uncommitted history; it changes no domain source.
	ddl := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.run_id='%s'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('%s',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER %s AFTER INSERT ON model_request_run_steps FOR EACH ROW EXECUTE FUNCTION %s()`, name, id, key, name, name)
	if _, e = b.pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := b.pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON model_request_run_steps; DROP FUNCTION IF EXISTS %s()`, name, name)); e != nil {
			t.Error("owned barrier cleanup", e)
		}
	}()
	ticket, e := f.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	created := make(chan error, 1)
	go func() {
		_, _, e := b.store.CreateOwnLocalModelRun(ctx, f.f.native.access, id, bindings, f.gate, ticket)
		created <- e
	}()
	var createPID int
	for {
		e = b.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%INSERT INTO model_request_run_steps%' LIMIT 1`, holderPID).Scan(&createPID)
		if e == nil {
			break
		}
		if e != pgx.ErrNoRows {
			t.Fatal(e)
		}
		select {
		case <-ctx.Done():
			t.Fatal("Create never held last-step barrier", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "088_model_request_runs.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	var downPID int
	if e = conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID); e != nil {
		t.Fatal(e)
	}
	downgraded := make(chan error, 1)
	go func() {
		_, e := conn.Exec(ctx, string(down))
		_, rollback := conn.Exec(context.Background(), `ROLLBACK`)
		if e == nil && rollback != nil {
			e = rollback
		}
		downgraded <- e
	}()
	for {
		var waiting bool
		e = b.pool.QueryRow(ctx, `SELECT $2=ANY(pg_blocking_pids($1))`, downPID, createPID).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-downgraded:
			t.Fatal("down did not wait for uncommitted history", e)
		case <-ctx.Done():
			t.Fatal("down lock barrier missing", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if e = hold.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-created:
		if e != nil {
			t.Fatal("Create must commit before down", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case e = <-downgraded:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "P0001" {
		t.Fatal("down deleted or failed incorrectly", e)
	}
	control, e := b.store.ReadOwnLocalModelRun(ctx, f.f.native.access, id)
	if e != nil || len(control.Steps) != 1 || control.State != modelrequestrun.RunPlanned {
		t.Fatal("history lost after waiting down", control, e)
	}
	retryAssertOriginalBudgets(t, f, 0)
	t.Logf("actual Create pid=%d down pid=%d blocked by real relation locks; committed originalRun=%s retained", createPID, downPID, id)
}

type modelRunOSInput struct {
	Digest   [32]byte
	RunID    string
	Bindings []modelegressbudget.LocalRetryBinding
	Barrier  string
	Mode     string
}
type modelRunOSReceipt struct {
	PID       int
	ExeHash   string
	Run       modelrequestrun.Control
	Calls     int32
	Operation modelegressbudget.AttemptControl
}

func writeModelRunOSReceipt(t *testing.T, r modelRunOSReceipt) {
	t.Helper()
	body, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	output := os.Getenv("BIRDTIE_MODEL_RUN_OS_OUTPUT")
	f, e := os.OpenFile(output+".pending", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(body); e != nil {
		t.Fatal(e)
	}
	if e = f.Sync(); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(output+".pending", output); e != nil {
		t.Fatal(e)
	}
}

func modelRunAccountingSnapshot(t *testing.T, f *egressFixture, id string) string {
	t.Helper()
	b := f.f.native.private.base
	var result string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
 'accounts',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) ORDER BY x.scope),'[]') FROM model_budget_accounts x WHERE owner_id=$1),
 'tasks',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) ORDER BY task_id),'[]') FROM model_budget_tasks x WHERE owner_id=$1),
 'root',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) ORDER BY root_trace_id),'[]') FROM model_budget_roots x WHERE owner_id=$1),
 'reservations',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) ORDER BY operation_id),'[]') FROM model_budget_reservations x WHERE owner_id=$1),
 'audit',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) ORDER BY id),'[]') FROM model_budget_audit x WHERE owner_id=$1),
 'run',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text)),'[]') FROM model_request_runs x WHERE id=$2),
 'steps',(SELECT COALESCE(jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) ORDER BY ordinal),'[]') FROM model_request_run_steps x WHERE run_id=$2))::text`, b.person.ID, id).Scan(&result)
	if e != nil {
		t.Fatal("owned accounting snapshot", e)
	}
	return result
}
func TestModelRequestRunNativePoolWaitCurrentFullPlanNoPartialAccounting(t *testing.T) {
	for _, kind := range []string{"taskABA", "revokeB", "sessionExpiry", "cancelFence", "leaseExpiry", "context"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			alt := retryAlternatePreview(t, f, "fallback", true)
			id := modelRunID(t, f)
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.MaxConns = 1
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			first := &nativeLocalAttemptAdapter{}
			second := &nativeRetryAdapter{}
			second.model = "fallback"
			bindings := []modelegressbudget.LocalRetryBinding{{Input: nativeAttemptInput(t, f, p), Destination: first.LocalDestination()}, {Input: nativeAttemptInput(t, f, alt), Destination: second.LocalDestination()}}
			ticket, e := f.gate.Capture(agentfeature.Enrichment)
			if e != nil {
				t.Fatal(e)
			}
			h, _, e := store.CreateOwnLocalModelRun(ctx, f.f.native.access, id, bindings, f.gate, ticket)
			if e != nil {
				t.Fatal(e)
			}
			hold, e := pool.Acquire(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Release()
			beforeWait := pool.Stat().EmptyAcquireCount()
			beforeCancelled := pool.Stat().CanceledAcquireCount()
			callctx, callcancel := context.WithCancel(ctx)
			defer callcancel()
			done := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				_, e := h.ReserveOwnModelAttempt(callctx, f.f.native.access, bindings[0].Input, f.gate)
				done <- e
			}()
			<-started
			select {
			case e := <-done:
				t.Fatal("Reserve did not wait on exclusive pool", e)
			case <-time.After(30 * time.Millisecond):
			}
			switch kind {
			case "taskABA":
				b.exec(`UPDATE agent_tasks SET query=query||' changed' WHERE id=$1`, p.TaskID)
				b.exec(`UPDATE agent_tasks SET query=$2 WHERE id=$1`, p.TaskID, p.Request.Messages[len(p.Request.Messages)-1].Content)
			case "revokeB":
				if e = b.store.RevokeOwnModelEgress(ctx, f.f.native.access, alt.ID); e != nil {
					t.Fatal(e)
				}
			case "sessionExpiry":
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '35 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
				time.Sleep(65 * time.Millisecond)
			case "cancelFence":
				current, e := b.store.ReadOwnLocalModelRun(ctx, f.f.native.access, id)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = b.store.CancelOwnLocalModelRun(ctx, f.f.native.access, id, current.Revision); e != nil {
					t.Fatal(e)
				}
			case "leaseExpiry":
				b.exec(`UPDATE model_request_runs SET lease_until=clock_timestamp()+interval '35 milliseconds',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id)
				time.Sleep(65 * time.Millisecond)
			case "context":
				callcancel()
			}
			before := modelRunAccountingSnapshot(t, f, id)
			hold.Release()
			select {
			case e = <-done:
			case <-ctx.Done():
				t.Fatal("Reserve never returned", ctx.Err())
			}
			want := modelegressbudget.ErrDenied
			if kind == "context" {
				want = context.Canceled
			}
			if !errors.Is(e, want) {
				t.Fatal("wait revived original dispatch/current source", kind, e)
			}
			if kind == "context" {
				if pool.Stat().CanceledAcquireCount() <= beforeCancelled {
					t.Fatal("no actual canceled pool acquisition")
				}
			} else if pool.Stat().EmptyAcquireCount() <= beforeWait {
				t.Fatal("no actual completed pool wait")
			}
			if modelRunAccountingSnapshot(t, f, id) != before {
				t.Fatal("rejected wait changed original ledger/Run/Step/audit")
			}
		})
	}
}
func TestModelRequestRunNativeCrossOwnerReverseOperationPlanNoDeadlockOrPartial(t *testing.T) {
	a := newEgressFixture(t, 3)
	b := newSecondModelRunOwner(t, 3, a.f.route.Revision)
	pa := a.preview(t, a.f.native.task.ID, true)
	pb := b.preview(t, b.f.native.task.ID, true)
	aa := a.f.native.private.base
	bb := b.f.native.private.base
	ba := modelRunBindings(t, a, pa, 2)
	bc := modelRunBindings(t, b, pb, 2)
	bc[0].Input.OperationID = ba[1].Input.OperationID
	bc[1].Input.OperationID = ba[0].Input.OperationID
	ids := []string{modelRunID(t, a), modelRunID(t, b)}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, 2)
	for i, f := range []*egressFixture{a, b} {
		bindings := ba
		if i == 1 {
			bindings = bc
		}
		ticket, e := f.gate.Capture(agentfeature.Enrichment)
		if e != nil {
			t.Fatal(e)
		}
		go func(f *egressFixture, id string, bindings []modelegressbudget.LocalRetryBinding, ticket agentfeature.Ticket) {
			<-start
			_, _, e := f.f.native.private.base.store.CreateOwnLocalModelRun(ctx, f.f.native.access, id, bindings, f.gate, ticket)
			done <- e
		}(f, ids[i], bindings, ticket)
	}
	close(start)
	ok, conflict := 0, 0
	for i := 0; i < 2; i++ {
		select {
		case e := <-done:
			if e == nil {
				ok++
			} else if errors.Is(e, modelegressbudget.ErrConflict) {
				conflict++
			} else {
				t.Fatal("reverse plan failed lock order", e)
			}
		case <-ctx.Done():
			t.Fatal("reverse plans deadlocked", ctx.Err())
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	retryAssertOriginalBudgets(t, a, 0)
	retryAssertOriginalBudgets(t, b, 0)
	var rows int
	if e := aa.pool.QueryRow(ctx, `SELECT count(*) FROM model_request_runs WHERE id=ANY($1::uuid[])`, ids).Scan(&rows); e != nil || rows != 1 {
		t.Fatal("partial conflicting run", e, rows)
	}
	// No cross-owner ledger/Run metadata becomes readable as authority.
	if _, e := bb.store.ReadOwnLocalModelRun(ctx, b.f.native.access, ids[0]); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("cross-owner run read", e)
	}
}

func TestModelRequestRunNativeAuditWaitAfterReservationRollsBackWholeAttempt(t *testing.T) {
	for _, point := range []string{"RESERVE", "IN_FLIGHT"} {
		t.Run(point, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			id := modelRunID(t, f)
			bindings := modelRunBindings(t, f, p, 1)
			h, _ := modelRunPlan(t, f, id, bindings)
			op := bindings[0].Input.OperationID
			if point == "IN_FLIGHT" {
				if _, e := h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[0].Input, f.gate); e != nil {
					t.Fatal(e)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			key := "model-run-audit:" + op
			name := "model_run_audit_" + strings.ReplaceAll(op, "-", "")
			hold, e := b.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Rollback(context.Background())
			if _, e = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); e != nil {
				t.Fatal(e)
			}
			var holder int
			if e = hold.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); e != nil {
				t.Fatal(e)
			}
			ddl := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation_id='%s'::uuid AND NEW.decision='%s' THEN PERFORM pg_advisory_xact_lock(hashtextextended('%s',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER %s AFTER INSERT ON model_budget_audit FOR EACH ROW EXECUTE FUNCTION %s()`, name, op, point, key, name, name)
			if _, e = b.pool.Exec(ctx, ddl); e != nil {
				t.Fatal(e)
			}
			defer func() {
				if _, e := b.pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON model_budget_audit; DROP FUNCTION IF EXISTS %s()`, name, name)); e != nil {
					t.Error("audit barrier cleanup", e)
				}
			}()
			// Native Session lifetime expires while its SHARE lock prevents refresh.
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '700 milliseconds' WHERE id=$1`, f.f.native.private.ownerSession)
			before := modelRunAccountingSnapshot(t, f, id)
			done := make(chan error, 1)
			go func() {
				if point == "RESERVE" {
					_, e := h.ReserveOwnModelAttempt(ctx, f.f.native.access, bindings[0].Input, f.gate)
					done <- e
				} else {
					_, e := h.BeginOwnLocalModelAttempt(ctx, f.f.native.access, op, f.gate)
					done <- e
				}
			}()
			var pid int
			for {
				e = b.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%model_budget_audit%' LIMIT 1`, holder).Scan(&pid)
				if e == nil {
					break
				}
				if e != pgx.ErrNoRows {
					t.Fatal(e)
				}
				select {
				case e := <-done:
					t.Fatal("writer never reached after-write audit wait", e)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			if _, e = hold.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM(idle_expires_at-clock_timestamp())))+0.02) FROM sessions WHERE id=$1`, f.f.native.private.ownerSession); e != nil {
				t.Fatal(e)
			}
			if e = hold.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !errors.Is(e, modelegressbudget.ErrDenied) {
				t.Fatal("after-audit natural Session expiry did not deny", e)
			}
			if modelRunAccountingSnapshot(t, f, id) != before {
				t.Fatal("after-write denial retained reservation/counters/step/audit mutation")
			}
			t.Logf("actual %s audit writer pid=%d waited for blocker=%d; original Session expired, full owned ledger+audit+Run/Step xmin unchanged", point, pid, holder)
		})
	}
}

// Child credentials are transferred only on stdin, never persisted/logged.
func TestModelRequestRunNativeOSChild(t *testing.T) {
	if os.Getenv("BIRDTIE_MODEL_RUN_OS_CHILD") != "1" {
		return
	}
	var in modelRunOSInput
	if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
		t.Fatal("child input", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	a := agentevent.Access{SessionDigest: in.Digest}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	binary, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	r := modelRunOSReceipt{PID: os.Getpid(), ExeHash: fmt.Sprintf("%x", sha256.Sum256(binary))}
	if in.Mode == "read" {
		r.Run, e = s.ReadOwnLocalModelRun(ctx, a, in.RunID)
		if e != nil {
			t.Fatal(e)
		}
		r.Operation, e = s.ReadOwnLocalModelAttempt(ctx, a, in.Bindings[0].Input.OperationID)
		if e != nil {
			t.Fatal(e)
		}
		writeModelRunOSReceipt(t, r)
		return
	}
	gate := egressGate(t, true)
	ticket, e := gate.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	h, _, e := s.CreateOwnLocalModelRun(ctx, a, in.RunID, in.Bindings, gate, ticket)
	if e != nil {
		t.Fatal(e)
	}
	op := in.Bindings[0].Input.OperationID
	if _, e = h.ReserveOwnModelAttempt(ctx, a, in.Bindings[0].Input, gate); e != nil {
		t.Fatal(e)
	}
	if in.Barrier != "reserved" {
		request, e := h.BeginOwnLocalModelAttempt(ctx, a, op, gate)
		if e != nil {
			t.Fatal(e)
		}
		if in.Barrier != "begun" {
			if _, e = h.CheckOwnLocalModelAttemptDispatch(ctx, a, op, gate, request, in.Bindings[0].Destination); e != nil {
				t.Fatal(e)
			}
			adapter := &nativeLocalAttemptAdapter{}
			harness, e := modelgateway.NewOfflineHarness(adapter)
			if e != nil {
				t.Fatal(e)
			}
			result, e := harness.Complete(ctx, request)
			if e != nil {
				t.Fatal(e)
			}
			r.Calls = adapter.calls.Load()
			if in.Barrier == "settled" {
				usage, e := modelegressbudget.NewLocalUsage(result.Usage)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = h.SettleOwnLocalModelAttempt(ctx, a, op, usage); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	r.Run, e = s.ReadOwnLocalModelRun(ctx, a, in.RunID)
	if e != nil {
		t.Fatal(e)
	}
	r.Operation, e = s.ReadOwnLocalModelAttempt(ctx, a, op)
	if e != nil {
		t.Fatal(e)
	}
	writeModelRunOSReceipt(t, r)
	// Actual process is intentionally killed here. No deferred code can replay.
	select {}
}
func TestModelRequestRunNativeFourActualOSKillAndRestartControlOnly(t *testing.T) {
	for _, point := range []string{"reserved", "begun", "returned", "settled"} {
		t.Run(point, func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			id := modelRunID(t, f)
			bindings := modelRunBindings(t, f, p, 2)
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			binary, e := os.ReadFile(exe)
			if e != nil {
				t.Fatal(e)
			}
			exeHash := fmt.Sprintf("%x", sha256.Sum256(binary))
			dir := t.TempDir()
			receipt := filepath.Join(dir, "before-kill.json")
			in := modelRunOSInput{Digest: f.f.native.access.SessionDigest, RunID: id, Bindings: bindings, Barrier: point, Mode: "execute"}
			input, e := json.Marshal(in)
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestModelRequestRunNativeOSChild$", "-test.v")
			cmd.Env = append(os.Environ(), "BIRDTIE_MODEL_RUN_OS_CHILD=1", "BIRDTIE_MODEL_RUN_OS_OUTPUT="+receipt)
			cmd.Stdin = strings.NewReader(string(input))
			var log bytes.Buffer
			cmd.Stdout = &log
			cmd.Stderr = &log
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			var before modelRunOSReceipt
			for {
				wire, err := os.ReadFile(receipt)
				if err == nil && json.Unmarshal(wire, &before) == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("child barrier timeout", ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if before.PID != cmd.Process.Pid || before.ExeHash != exeHash || filepath.Clean(cmd.Path) != filepath.Clean(exe) {
				t.Fatal("owned child identity mismatch")
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			if e = cmd.Wait(); e == nil {
				t.Fatal("child was not actually killed")
			}
			wanted := "IN_FLIGHT"
			calls := int32(0)
			if point == "reserved" {
				wanted = "RESERVED"
			}
			if point == "returned" {
				calls = 1
			}
			if point == "settled" {
				wanted = "SETTLED"
				calls = 1
			}
			if before.Operation.State != wanted || before.Calls != calls || before.Run.State != modelrequestrun.RunRunning || before.Run.Steps[1].State != modelrequestrun.StepPlanned {
				t.Fatal("wrong kill checkpoint", before)
			}
			var original string
			if e = b.pool.QueryRow(b.ctx, `SELECT row_to_json(r)::text||':'||xmin::text FROM model_budget_reservations r WHERE operation_id=$1`, bindings[0].Input.OperationID).Scan(&original); e != nil {
				t.Fatal(e)
			}
			in.Mode = "read"
			input, e = json.Marshal(in)
			if e != nil {
				t.Fatal(e)
			}
			output := filepath.Join(dir, "after-restart.json")
			restart := exec.CommandContext(ctx, exe, "-test.run=^TestModelRequestRunNativeOSChild$", "-test.v")
			restart.Env = append(os.Environ(), "BIRDTIE_MODEL_RUN_OS_CHILD=1", "BIRDTIE_MODEL_RUN_OS_OUTPUT="+output)
			restart.Stdin = strings.NewReader(string(input))
			raw, e := restart.CombinedOutput()
			if e != nil {
				t.Fatal("recovery process", e, string(raw))
			}
			wire, e := os.ReadFile(output)
			if e != nil {
				t.Fatal(e)
			}
			var after modelRunOSReceipt
			if json.Unmarshal(wire, &after) != nil || after.PID == before.PID || after.ExeHash != exeHash || after.Operation != before.Operation || after.Calls != 0 || after.Run.State != modelrequestrun.RunRunning {
				t.Fatal("restart continued or regenerated result", after)
			}
			var unchanged string
			if e = b.pool.QueryRow(b.ctx, `SELECT row_to_json(r)::text||':'||xmin::text FROM model_budget_reservations r WHERE operation_id=$1`, bindings[0].Input.OperationID).Scan(&unchanged); e != nil || unchanged != original {
				t.Fatal("recovery changed original budget", e)
			}
			retryAssertOriginalBudgets(t, f, 1)
			if point == "reserved" {
				current, e := b.store.CancelOwnLocalModelRun(b.ctx, f.f.native.access, id, after.Run.Revision)
				if e != nil || current.Steps[0].State != modelrequestrun.StepCancelled {
					t.Fatal("explicit reserved recovery cancellation", current, e)
				}
			} else {
				if _, e = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, bindings[0].Input.OperationID); !errors.Is(e, modelegressbudget.ErrConflict) {
					t.Fatal("recovery refunded possibly sent attempt", e)
				}
			}
			t.Logf("LOCAL_SYNTHETIC barrier=%s killedPID=%d restartedPID=%d exeSHA256=%s originalOperation=%s state=%s priorLocalCalls=%d recoveryCalls=0", point, before.PID, after.PID, exeHash, bindings[0].Input.OperationID, wanted, calls)
		})
	}
}
func modelRunPlan(t *testing.T, f *egressFixture, id string, bindings []modelegressbudget.LocalRetryBinding) (modelegressbudget.LocalModelRunHandle, modelrequestrun.Control) {
	t.Helper()
	ticket, e := f.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	b := f.f.native.private.base
	h, c, e := b.store.CreateOwnLocalModelRun(b.ctx, f.f.native.access, id, bindings, f.gate, ticket)
	if e != nil {
		t.Fatal("native model run", e)
	}
	if e = modelrequestrun.ValidateControl(c, time.Now()); e != nil {
		t.Fatal("native control shape", e)
	}
	return h, c
}
func modelRunBindings(t *testing.T, f *egressFixture, p modelegressbudget.Preview, n int) []modelegressbudget.LocalRetryBinding {
	t.Helper()
	a := &nativeLocalAttemptAdapter{}
	out := make([]modelegressbudget.LocalRetryBinding, n)
	for i := range out {
		out[i] = modelegressbudget.LocalRetryBinding{Input: nativeAttemptInput(t, f, p), Destination: a.LocalDestination()}
	}
	return out
}
func TestModelRequestRunNativePlanDoesNotReserveFallbackAndLegacyCannotBypass(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	id := modelRunID(t, f)
	bindings := modelRunBindings(t, f, p, 2)
	h, c := modelRunPlan(t, f, id, bindings)
	if c.State != modelrequestrun.RunPlanned || len(c.Steps) != 2 {
		t.Fatal(c)
	}
	retryAssertOriginalBudgets(t, f, 0)
	if _, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[0].Input, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("legacy reserve bypass", e)
	}
	if _, e := h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[1].Input, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("fallback before native failure", e)
	}
	if _, e := h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[0].Input, f.gate); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, bindings[0].Input.OperationID, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("legacy begin bypass", e)
	}
	request, e := h.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, bindings[0].Input.OperationID, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	if request.RunID != f.binding || request.RunID == id {
		t.Fatal("rewrote original binding")
	}
	if _, e = b.store.CheckOwnLocalModelAttemptDispatch(b.ctx, f.f.native.access, bindings[0].Input.OperationID, f.gate, request, bindings[0].Destination); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("legacy dispatch bypass", e)
	}
	if _, e = h.CheckOwnLocalModelAttemptDispatch(b.ctx, f.f.native.access, bindings[0].Input.OperationID, f.gate, request, bindings[0].Destination); e != nil {
		t.Fatal(e)
	}
	// Unknown accounting is not a normative terminal outcome or retry receipt.
	if _, e = b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, bindings[0].Input.OperationID, modelegressbudget.LocalUsage{}); e != nil {
		t.Fatal(e)
	}
	current, e := b.store.ReadOwnLocalModelRun(b.ctx, f.f.native.access, id)
	if e != nil || current.State != modelrequestrun.RunRunning || current.Steps[0].State != modelrequestrun.StepUnknown || current.Steps[1].State != modelrequestrun.StepPlanned {
		t.Fatal("accounting forged result", current, e)
	}
	if _, e = h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[1].Input, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("unknown metadata minted fallback", e)
	}
	retryAssertOriginalBudgets(t, f, 1)
	wire, e := json.Marshal(current)
	if e != nil {
		t.Fatal(e)
	}
	if len(wire) == 0 {
		t.Fatal("missing control")
	}
	if _, e = json.Marshal(h); !errors.Is(e, modelegressbudget.ErrServerOnly) {
		t.Fatal("claim serialized", e)
	}
}
func TestModelRequestRunNativeDriverCompletesSameLedgerAndUnusedFallback(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "unusedFallback", true: "explicitFailure"}[retry], func(t *testing.T) {
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			id := modelRunID(t, f)
			first := &nativeRetryAdapter{}
			if retry {
				first.failure = modelgateway.ProviderError{Code: "RATE_LIMIT", Retryable: true}
			}
			next := &nativeRetryAdapter{}
			steps := []modelegressbudget.LocalRetryStep{retryNativeStep(t, f, b.store, p, first), retryNativeStep(t, f, b.store, p, next)}
			runner := modelegressbudget.NewLocalModelRunRunner(b.store, f.gate, retryNativePolicy())
			if runner == nil {
				t.Fatal("runner unavailable")
			}
			out, e := runner.Run(b.ctx, f.f.native.access, id, steps)
			if e != nil || len(out.EncodedResult) == 0 {
				t.Fatal("real native run failed", e)
			}
			control, e := b.store.ReadOwnLocalModelRun(b.ctx, f.f.native.access, id)
			if e != nil || control.State != modelrequestrun.RunFinished || first.calls.Load() != 1 {
				t.Fatal(control, e)
			}
			wanted := int64(1)
			if retry {
				wanted = 2
				if next.calls.Load() != 1 || control.Steps[0].State != modelrequestrun.StepUnknown || control.Steps[1].State != modelrequestrun.StepSettled {
					t.Fatal("explicit retry states", control)
				}
			} else if next.calls.Load() != 0 || control.Steps[1].State != modelrequestrun.StepPlanned {
				t.Fatal("unused fallback charged", control)
			}
			retryAssertOriginalBudgets(t, f, wanted)
			if _, e = runner.Run(b.ctx, f.f.native.access, id, steps); !errors.Is(e, modelegressbudget.ErrConflict) {
				t.Fatal("restart reconstructed old answer", e)
			}
		})
	}
}
func TestModelRequestRunNativeCancelAndAccountingCannotRelease(t *testing.T) {
	for _, begun := range []bool{false, true} {
		t.Run(map[bool]string{false: "reserved", true: "inFlight"}[begun], func(t *testing.T) {
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			id := modelRunID(t, f)
			bindings := modelRunBindings(t, f, p, 2)
			h, _ := modelRunPlan(t, f, id, bindings)
			op := bindings[0].Input.OperationID
			if _, e := h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[0].Input, f.gate); e != nil {
				t.Fatal(e)
			}
			if begun {
				if _, e := h.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, op, f.gate); e != nil {
					t.Fatal(e)
				}
			}
			before, e := b.store.ReadOwnLocalModelRun(b.ctx, f.f.native.access, id)
			if e != nil {
				t.Fatal(e)
			}
			after, e := b.store.CancelOwnLocalModelRun(b.ctx, f.f.native.access, id, before.Revision)
			if e != nil || after.State != modelrequestrun.RunCancelled || after.Fence <= before.Fence {
				t.Fatal(after, e)
			}
			if _, e = h.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, op, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
				t.Fatal("cancelled claim revived", e)
			}
			if begun {
				if _, e = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, op); !errors.Is(e, modelegressbudget.ErrConflict) {
					t.Fatal("inflight refunded", e)
				}
				if _, e = b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, op, modelegressbudget.LocalUsage{}); e != nil {
					t.Fatal("terminal reconciliation", e)
				}
			}
			result, e := b.store.ReadOwnLocalModelRun(b.ctx, f.f.native.access, id)
			if e != nil || result.State != modelrequestrun.RunCancelled {
				t.Fatal(result, e)
			}
			retryAssertOriginalBudgets(t, f, 1)
		})
	}
}
func TestModelRequestRunNativePriorReservationAndConcurrentPlansRejected(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	bindings := modelRunBindings(t, f, p, 2)
	if _, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[0].Input, f.gate); e != nil {
		t.Fatal(e)
	}
	ticket, e := f.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = b.store.CreateOwnLocalModelRun(b.ctx, f.f.native.access, modelRunID(t, f), bindings, f.gate, ticket); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("adopted prior reservation", e)
	}
	bindings = modelRunBindings(t, f, p, 2)
	ids := []string{modelRunID(t, f), modelRunID(t, f)}
	var wg sync.WaitGroup
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			_, _, e := b.store.CreateOwnLocalModelRun(b.ctx, f.f.native.access, id, bindings, f.gate, ticket)
			done <- e
		}(id)
	}
	close(start)
	wg.Wait()
	close(done)
	ok, conflicts := 0, 0
	for e := range done {
		if e == nil {
			ok++
		} else if errors.Is(e, modelegressbudget.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal("two plans adopted same op", ok, conflicts)
	}
	retryAssertOriginalBudgets(t, f, 1)
}
func TestModelRequestRunNativeSQLNullReservationRejectedAndUsedDownProtected(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	id := modelRunID(t, f)
	bindings := modelRunBindings(t, f, p, 1)
	_, _ = modelRunPlan(t, f, id, bindings)
	for _, q := range []string{`UPDATE model_request_run_steps SET state='RESERVED',reservation_id=NULL,updated_at=clock_timestamp() WHERE run_id=$1`, `INSERT INTO model_request_run_steps(run_id,ordinal,operation_id,preview_id,price_version,request_digest,state,reservation_id) SELECT run_id,2,gen_random_uuid(),preview_id,price_version,request_digest,'RESERVED',NULL FROM model_request_run_steps WHERE run_id=$1`} {
		_, e := b.pool.Exec(b.ctx, q, id)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23514" {
			t.Fatal("SQL accepted nonplanned NULL reference", e)
		}
	}
	// Execute the exact guarded down prefix, without accidentally dropping any
	// tables if a future assertion fails. Actual full unused down is runner-owned.
	_, e := b.pool.Exec(b.ctx, `DO $$ BEGIN IF EXISTS(SELECT 1 FROM model_request_runs) OR EXISTS(SELECT 1 FROM model_request_run_steps) THEN RAISE EXCEPTION 'model request run history must be retained'; END IF; END $$`)
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "P0001" {
		t.Fatal("used down not protected", e)
	}
}

// The second independent owner activates its registered immutable version with
// the actual existing route revision; it never bypasses the original CAS.
func newSecondModelRunOwner(t *testing.T, requests, expectedRouteRevision int64) *egressFixture {
	t.Helper()
	f := &egressFixture{f: configurationNativeFixture(t), gate: egressGate(t, true)}
	b := f.f.native.private.base
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		for _, q := range []string{`DELETE FROM model_budget_audit WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_egress_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_tasks WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_roots WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_accounts WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Errorf("owned budget cleanup: %v", e)
			}
		}
		if f.price.Version != "" {
			if _, e := b.pool.Exec(ctx, `DELETE FROM model_local_price_versions WHERE version=$1`, f.price.Version); e != nil {
				t.Error(e)
			}
		}
	})
	route := activateConfigurationFixture(t, f.f, 0, expectedRouteRevision)
	f.binding = egressID(t, f)
	r := f.f.request(t, 0, f.binding)
	if _, e := b.store.BindModelTaskConfiguration(b.ctx, f.f.native.access, f.f.native.task.ID, route.Version, route.Revision, r); e != nil {
		t.Fatal("real pre-run binding", e)
	}
	f.price = modelegressbudget.Price{Version: "price_" + strings.ReplaceAll(b.person.ID, "-", ""), Destination: modelcapability.Key{Provider: "fake", Model: "local", Version: "v1", WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: modelegressbudget.Retention, Currency: "GBP", InputMicrosPerToken: 2, OutputMicrosPerToken: 3, InputTokenCeiling: 100, OutputTokenCeiling: 128, Evidence: modelegressbudget.LocalPrice, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	if e := b.store.RegisterLocalModelPrice(b.ctx, f.price); e != nil {
		t.Fatal("real immutable synthetic price registration", e)
	}
	f.limits = modelegressbudget.Limits{Requests: requests, InputTokens: 100000, OutputTokens: 100000, CostMicros: 10000000}
	if e := b.store.ConfigureOwnModelBudget(b.ctx, f.f.native.access, f.f.native.task.ID, f.binding, "GBP", f.limits, f.limits); e != nil {
		t.Fatal("native budget account", e)
	}
	f.root = egressID(t, f)
	in := modelegressbudget.RootInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, BindingID: f.binding, Currency: "GBP", Limits: f.limits, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)}
	if e := b.store.CreateOwnModelBudgetRoot(b.ctx, f.f.native.access, in); e != nil {
		t.Fatal("native root", e)
	}
	return f
}
