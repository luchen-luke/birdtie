package postgres

import (
	"context"
	"encoding/json"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type actionCrashInput struct {
	SessionDigest                                                      [32]byte
	TaskID, Operation, ActionID, OwnerID, DispatchID, Phase, ReplyPath string
}

// Test-only process exit after the ACTUAL successful sandbox transaction,
// before its native Execute caller can acknowledge or reconcile that write.
type actionCrashTracer struct{ effect bool }

func (tr *actionCrashTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "INSERT INTO agent_sandbox_writes") {
		tr.effect = true
	}
	return context.WithValue(ctx, actionCrashKey{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}

type actionCrashKey struct{}

func (tr *actionCrashTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	isCommit, _ := ctx.Value(actionCrashKey{}).(bool)
	if tr.effect && isCommit && d.Err == nil {
		os.Exit(0)
	}
}
func TestAgentActionNativeRecoveryProcess(t *testing.T) {
	path := os.Getenv("BIRDTIE_ACTION_CRASH_PRIVATE_INPUT")
	if path == "" {
		return
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var in actionCrashInput
	if json.Unmarshal(raw, &in) != nil {
		t.Fatal("private subprocess input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	if in.Phase == "applied" {
		cfg.ConnConfig.Tracer = &actionCrashTracer{}
	}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	gate := egressGate(t, true)
	if in.Phase == "recover" {
		d, e := s.MarkOwnSandboxUnknown(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, in.DispatchID, gate)
		if e != nil || d.State != aa.Unknown {
			t.Fatal("actual restarted unknown state", e, d)
		}
		result, e := s.ReconcileOwnSandboxDispatch(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, in.DispatchID, gate)
		if e != nil {
			t.Fatal(e)
		}
		v, _ := json.Marshal(result)
		if os.WriteFile(in.ReplyPath, v, 0600) != nil {
			t.Fatal("recovery metadata reply")
		}
		return
	}
	g, e := s.PrepareOwnReadonlyPlan(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, in.TaskID, in.Operation)
	if e != nil {
		t.Fatal(e)
	}
	native, ok := g.(*nativePlannerGoal)
	if !ok {
		t.Fatal("native current restarted goal")
	}
	// Every subprocess derives a NEW native goal from the actual current DB,
	// never deserializes an executable handle or grants a JSON confirmed flag.
	input := xActionProposal(in.ActionID, in.Operation, in.OwnerID, native.source)
	p, e := s.PreviewOwnSandboxAction(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, g, input, gate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApproveOwnSandboxAction(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, p.Binding.ApprovalID, p.BindingDigest, gate); e != nil {
		t.Fatal(e)
	}
	d, h, e := s.CommitOwnSandboxAction(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, p.Binding.ApprovalID, input, gate)
	if e != nil || h == nil {
		t.Fatal(e)
	}
	reply, _ := json.Marshal(d)
	if os.WriteFile(in.ReplyPath, reply, 0600) != nil {
		t.Fatal("commit metadata reply")
	}
	if in.Phase == "commit" {
		os.Exit(0)
	}
	_, cl, e := h.Begin(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, gate)
	if e != nil || cl == nil {
		t.Fatal(e)
	}
	if in.Phase == "begin" {
		os.Exit(0)
	}
	if _, e = cl.Execute(ctx, agentevent.Access{SessionDigest: in.SessionDigest}, gate); e != nil {
		t.Fatal(e)
	}
	t.Fatal("actual sandbox commit exit point not reached")
}
func TestAgentActionNativeActualProcessCrashAtCommitBeginAndAppliedNoResend(t *testing.T) {
	for _, phase := range []string{"commit", "begin", "applied"} {
		t.Run(phase, func(t *testing.T) {
			x := actionNativeFixture(t)
			f := x.f
			b := f.f.native.private.base
			dir := t.TempDir()
			private := filepath.Join(dir, "private-input.json")
			reply := filepath.Join(dir, "receipt.json")
			in := actionCrashInput{SessionDigest: f.f.native.access.SessionDigest, TaskID: f.f.native.task.ID, Operation: egressID(t, f), ActionID: egressID(t, f), OwnerID: b.person.ID, Phase: phase, ReplyPath: reply}
			run := func() {
				raw, _ := json.Marshal(in)
				if os.WriteFile(private, raw, 0600) != nil {
					t.Fatal("private fixture handoff")
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestAgentActionNativeRecoveryProcess$", "-test.timeout=25s")
				cmd.Env = append(os.Environ(), "BIRDTIE_ACTION_CRASH_PRIVATE_INPUT="+private)
				raw, e := cmd.CombinedOutput()
				if e != nil {
					t.Fatalf("actual child phase %s exit failed: %s %v", in.Phase, string(raw), e)
				}
				cwd, e := os.Getwd()
				if e != nil {
					t.Fatal(e)
				}
				t.Logf("actual child phase=%s command=%v cwd=%s exit=0 (auth input private temp file, not evidence)", in.Phase, cmd.Args, cwd)
				actionWire(t, "sandbox-child-command-"+phase+"-"+in.Phase, map[string]any{"phase": in.Phase, "command": cmd.Args, "cwd": cwd, "exit": 0, "fixture": "OWNED_NATIVE_SYNTHETIC_NOT_IDP_NOT_PILOT", "privateAuthInputRetainedInEvidence": false})
			}
			run()
			raw, e := os.ReadFile(reply)
			if e != nil {
				t.Fatal(e)
			}
			var d aa.Dispatch
			if json.Unmarshal(raw, &d) != nil {
				t.Fatal("native metadata reply")
			}
			in.DispatchID = d.ID
			in.Phase = "recover"
			run()
			raw, e = os.ReadFile(reply)
			if e != nil {
				t.Fatal(e)
			}
			var done aa.Dispatch
			if json.Unmarshal(raw, &done) != nil {
				t.Fatal("native recovered receipt")
			}
			dc, wc := actionRows(t, x)
			expected := 0
			if phase == "applied" {
				expected = 1
				if done.State != aa.Succeeded {
					t.Fatal("applied effect lost after actual process crash", done)
				}
			} else if done.State != aa.NoEffect {
				t.Fatal("closed native fence without effect not acknowledged", done)
			}
			if dc != 1 || wc != expected {
				t.Fatal("restart resent or lost actual effect", dc, wc)
			}
			actionWire(t, "sandbox-crash-"+phase+"-recovered", done)
		})
	}
}
func TestAgentActionNativeUnknownFencesLateClaimAndForeignRecovery(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	actionApprove(t, x)
	d, h := actionCommitNative(t, x)
	_, cl, e := h.Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	restarted := New(b.pool, false)
	unknown, e := restarted.MarkOwnSandboxUnknown(b.ctx, f.f.native.access, d.ID, f.gate)
	if e != nil || unknown.State != aa.Unknown {
		t.Fatal("unknown not persisted", e, unknown)
	}
	if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e == nil {
		t.Fatal("late old claim escaped closed fence")
	}
	if _, e = restarted.ReconcileOwnSandboxDispatch(b.ctx, agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}, d.ID, f.gate); e == nil {
		t.Fatal("foreign receipt escaped")
	}
	absent := egressID(t, f)
	if r, e := restarted.ReconcileOwnSandboxDispatch(b.ctx, f.f.native.access, absent, f.gate); e == nil || r.State == aa.NoEffect || r.State == aa.Succeeded {
		t.Fatal("404 became authoritative result", e, r)
	}
	result, e := restarted.ReconcileOwnSandboxDispatch(b.ctx, f.f.native.access, d.ID, f.gate)
	if e != nil || result.State != aa.NoEffect {
		t.Fatal(e, result)
	}
	if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e == nil {
		t.Fatal("NO_EFFECT automatically resent")
	}
	if _, wc := actionRows(t, x); wc != 0 {
		t.Fatal("unknown recovery wrote")
	}
	actionWire(t, "sandbox-native-unknown", unknown)
	actionWire(t, "sandbox-native-authoritative-no-effect", result)
}

func actionWaitExpiry(t *testing.T, x *actionFixture) {
	t.Helper()
	b := x.f.f.native.private.base
	end := time.Now().Add(5 * time.Second)
	expired := false
	for time.Now().Before(end) {
		if e := b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM agent_action_approvals WHERE id=$1`, x.p.Binding.ApprovalID).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("actual database expiry not reached")
}
func TestAgentActionNativeExpiredApprovalAndLeaseNeverReissue(t *testing.T) {
	for _, phase := range []string{"approval", "lease"} {
		t.Run(phase, func(t *testing.T) {
			x := actionNativeFixtureLifetime(t, 2*time.Second)
			f := x.f
			b := f.f.native.private.base
			actionApprove(t, x)
			var d aa.Dispatch
			var cl aa.Claim
			if phase == "lease" {
				var h aa.Commitment
				d, h = actionCommitNative(t, x)
				var e error
				_, cl, e = h.Begin(b.ctx, f.f.native.access, f.gate)
				if e != nil {
					t.Fatal(e)
				}
			}
			actionWaitExpiry(t, x)
			if phase == "approval" {
				if d, h, e := b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate); e == nil || d.ID != "" || h != nil {
					t.Fatal("expired approval committed")
				}
				if dc, w := actionRows(t, x); dc != 0 || w != 0 {
					t.Fatal(dc, w)
				}
			} else {
				if _, e := cl.Execute(b.ctx, f.f.native.access, f.gate); e == nil {
					t.Fatal("expired lease performed write")
				}
				unknown, e := b.store.MarkOwnSandboxUnknown(b.ctx, f.f.native.access, d.ID, f.gate)
				if e != nil || unknown.State != aa.Unknown {
					t.Fatal(e, unknown)
				}
				result, e := b.store.ReconcileOwnSandboxDispatch(b.ctx, f.f.native.access, d.ID, f.gate)
				if e != nil || result.State != aa.NoEffect {
					t.Fatal(e, result)
				}
				if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e == nil {
					t.Fatal("lease recovery resent")
				}
				if _, w := actionRows(t, x); w != 0 {
					t.Fatal("expired actual effect")
				}
			}
		})
	}
}
