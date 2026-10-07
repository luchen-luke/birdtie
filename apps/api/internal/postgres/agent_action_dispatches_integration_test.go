package postgres

import (
	"context"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type actionDispatchWaitKey struct{}
type actionDispatchWaitTracer struct {
	armed           atomic.Bool
	arrived, resume chan struct{}
}

func (tr *actionDispatchWaitTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	match := strings.HasPrefix(d.SQL, "INSERT INTO audit_events") && len(d.Args) > 1 && d.Args[1] == "sandbox_dispatch"
	return context.WithValue(ctx, actionDispatchWaitKey{}, match)
}
func (tr *actionDispatchWaitTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	match, _ := ctx.Value(actionDispatchWaitKey{}).(bool)
	if match && tr.armed.CompareAndSwap(true, false) {
		close(tr.arrived)
		select {
		case <-tr.resume:
		case <-ctx.Done():
		}
	}
}
func TestAgentActionNativeActualClockAfterLastAuditWaitPreventsLateCommit(t *testing.T) {
	x := actionNativeFixtureLifetime(t, 2*time.Second)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	tr := &actionDispatchWaitTracer{arrived: make(chan struct{}), resume: make(chan struct{})}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Tracer = tr
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	tr.armed.Store(true)
	ctx, cancel := context.WithTimeout(b.ctx, 6*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		d, h, e := s.CommitOwnSandboxAction(ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate)
		if e == nil || d.ID != "" || h != nil {
			done <- aa.ErrInvalid
		} else {
			done <- nil
		}
	}()
	select {
	case <-tr.arrived:
	case <-time.After(time.Second):
		close(tr.resume)
		t.Fatal("actual audit wait point not reached")
	}
	actionWaitExpiry(t, x)
	close(tr.resume)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("expired last check still committed", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("actual last check did not return")
	}
	d, w := actionRows(t, x)
	if d != 0 || w != 0 {
		t.Fatal("last native clock allowed effect", d, w)
	}
	var consumed bool
	if e = b.pool.QueryRow(b.ctx, `SELECT consumed_at IS NOT NULL FROM agent_action_approvals WHERE id=$1`, x.p.Binding.ApprovalID).Scan(&consumed); e != nil || consumed {
		t.Fatal("expiry consumed original approval", e)
	}
}

type actionCommitLostTracer struct {
	armed  bool
	cancel context.CancelFunc
}

func (tr *actionCommitLostTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "INSERT INTO agent_action_dispatches") {
		tr.armed = true
	}
	return context.WithValue(ctx, actionCrashKey{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}
func (tr *actionCommitLostTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(actionCrashKey{}).(bool)
	if tr.armed && commit && d.Err == nil {
		tr.cancel()
		tr.armed = false
	}
}
func TestAgentActionNativeActualCommitAcknowledgementLostDoesNotCreateNewSender(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Tracer = &actionCommitLostTracer{cancel: cancel}
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	if d, h, e := s.CommitOwnSandboxAction(ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate); e != aa.ErrUnknown || d.ID != "" || h != nil {
		t.Fatal("lost confirmed COMMIT fabricated capability", e, d, h)
	}
	fresh := New(b.pool, false)
	d, h, e := fresh.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate)
	if e != nil || h != nil || d.State != aa.Committed {
		t.Fatal("retry after unknown sent again", e, d, h)
	}
	u, e := fresh.MarkOwnSandboxUnknown(b.ctx, f.f.native.access, d.ID, f.gate)
	if e != nil || u.State != aa.Unknown {
		t.Fatal(e, u)
	}
	done, e := fresh.ReconcileOwnSandboxDispatch(b.ctx, f.f.native.access, d.ID, f.gate)
	if e != nil || done.State != aa.NoEffect {
		t.Fatal(e, done)
	}
	if _, w := actionRows(t, x); w != 0 {
		t.Fatal("unknown commit recovery performed effect")
	}
	actionWire(t, "sandbox-native-lost-commit-unknown", u)
}

func TestAgentActionNativeGrantRevocationBeforeDispatchCommitWinsActualLockOrder(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var id string
	if e = tx.QueryRow(b.ctx, `SELECT id FROM consent_grants WHERE id=$1 FOR UPDATE`, x.p.Binding.GrantID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		d, h, e := b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate)
		if e == nil || d.ID != "" || h != nil {
			done <- aa.ErrInvalid
		} else {
			done <- nil
		}
	}()
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT revision=1 AND revoked_at IS NULL%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("actual native dispatch did not reach locked original grant")
	}
	if _, e = tx.Exec(b.ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("revocation losing race", e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("native dispatch did not resolve after actual revoke")
	}
	d, w := actionRows(t, x)
	if d != 0 || w != 0 {
		t.Fatal("revoke-first consumed or wrote", d, w)
	}
	var consumed bool
	if e = b.pool.QueryRow(b.ctx, `SELECT consumed_at IS NOT NULL FROM agent_action_approvals WHERE id=$1`, x.p.Binding.ApprovalID).Scan(&consumed); e != nil || consumed {
		t.Fatal("revoke-first consumed old approval", e)
	}
}
func TestAgentActionNativeDispatchCommitBeforeRevocationDoesNotPretendUndo(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	d, h := actionCommitNative(t, x)
	p, e := b.store.CancelOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, f.gate)
	if e != nil || p.State != aa.Consumed {
		t.Fatal("committed action falsely cancelled", e, p.State)
	}
	if dc, w := actionRows(t, x); dc != 1 || w != 0 {
		t.Fatal(dc, w)
	}
	_, cl, e := h.Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	result, e := cl.Execute(b.ctx, f.f.native.access, f.gate)
	if e != nil || result.State != aa.Succeeded {
		t.Fatal("already committed private effect not reconciled", e, result)
	}
	b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, f.f.native.task.ID)
	again, e := New(b.pool, false).ReadOwnSandboxDispatch(b.ctx, f.f.native.access, d.ID, f.gate)
	if e != nil || again.State != aa.Succeeded || again.EffectID == nil || *again.EffectID != *result.EffectID {
		t.Fatal("later stale source rewrote actual immutable receipt", e, again)
	}
	if _, h, e := b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate); e == nil || h != nil {
		t.Fatal("later revoked/stale source regained dispatch")
	}
	actionWire(t, "sandbox-native-commit-before-revoke", again)
}

func TestAgentActionNativeTwoWorkersConsumeOnce(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	type result struct {
		d aa.Dispatch
		h aa.Commitment
		e error
	}
	out := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s := New(b.pool, false)
			d, h, e := s.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate)
			out <- result{d, h, e}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	var handles []aa.Commitment
	var id string
	for r := range out {
		if r.e != nil {
			t.Fatal(r.e)
		}
		if id != "" && id != r.d.ID {
			t.Fatal("workers committed different dispatches")
		}
		id = r.d.ID
		if r.h != nil {
			handles = append(handles, r.h)
		}
	}
	if len(handles) != 1 {
		t.Fatal("exactly one native sender required", len(handles))
	}
	_, cl, e := handles[0].Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e != nil {
		t.Fatal(e)
	}
	d, w := actionRows(t, x)
	if d != 1 || w != 1 {
		t.Fatal(d, w)
	}
}
func TestAgentActionNativeDeliberateEqualNewOperationHasTwoEffects(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	_, h := actionCommitNative(t, x)
	_, cl, e := h.Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e != nil {
		t.Fatal(e)
	}
	g, p := toolSandboxGoal(t, f)
	p.Value = x.p.Proposal.Value
	p.ActionID = x.p.Proposal.ActionID
	if p.LogicalOperationID == x.p.Proposal.LogicalOperationID {
		t.Fatal("fixture did not create independent user operation")
	}
	if time.Now().After(g.View().ValidUntil) {
		t.Fatal("expired fixture")
	}
	p2, e := b.store.PreviewOwnSandboxAction(b.ctx, f.f.native.access, g, agenttool.SandboxProposal(p), f.gate)
	if e != nil {
		t.Fatal(e)
	}
	x2 := &actionFixture{f, p2}
	actionApprove(t, x2)
	_, h = actionCommitNative(t, x2)
	_, cl, e = h.Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e != nil {
		t.Fatal(e)
	}
	d, w := actionRows(t, x)
	if d != 2 || w != 2 {
		t.Fatal("equal intentional operation collapsed", d, w)
	}
}
