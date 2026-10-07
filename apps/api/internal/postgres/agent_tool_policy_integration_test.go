package postgres

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type toolPolicyTraceKey struct{}
type toolPolicyWaitTracer struct {
	armed           atomic.Bool
	final           atomic.Bool
	arrived, resume chan struct{}
}

func (t *toolPolicyWaitTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "p.observed_at<$") && strings.Contains(data.SQL, "agent_policy_settings tp") {
		t.final.Store(true)
	}
	match := strings.HasPrefix(data.SQL, "SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(p)")
	return context.WithValue(ctx, toolPolicyTraceKey{}, match)
}
func (t *toolPolicyWaitTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	match, _ := ctx.Value(toolPolicyTraceKey{}).(bool)
	if match && t.armed.CompareAndSwap(true, false) {
		close(t.arrived)
		select {
		case <-t.resume:
		case <-ctx.Done():
		}
	}
}
func TestAgentToolNativePolicyExpiryAfterActualCaptureBeforeFinalPayload(t *testing.T) {
	f := outputActivityFixture(t, 4)
	b := f.f.native.private.base
	tracer := &toolPolicyWaitTracer{arrived: make(chan struct{}), resume: make(chan struct{})}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	original := b.store
	b.store = New(pool, false)
	defer func() { b.store = original }()
	if _, e = b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(4*time.Second))); e != nil {
		t.Fatal(e)
	}
	id := outputPublishedActivity(t, f, "public")
	o, _ := toolNativePlanner(t, f, id)
	d, call, e := agenttool.NewService(o.ToolPlan()).Check(b.ctx, f.f.native.access, o.Plan.Actions[0], f.gate)
	if e != nil || d.Disposition != agenttool.Allow {
		t.Fatal("actual checked read prerequisite", e)
	}
	tracer.final.Store(false)
	tracer.armed.Store(true)
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	type outcome struct {
		result agenttool.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := call.Read(ctx, f.f.native.access, f.gate); done <- outcome{r, e} }()
	select {
	case <-tracer.arrived:
	case <-time.After(2 * time.Second):
		close(tracer.resume)
		t.Fatal("actual native policy hash query wasn't reached")
	}
	expired := false
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=expires_at FROM agent_policy_settings WHERE agent_id=$1 AND family='AUTONOMY'`, b.personID).Scan(&expired) != nil {
			close(tracer.resume)
			t.Fatal("actual PG expiry clock unavailable")
		}
		if expired {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(tracer.resume)
	if !expired {
		t.Fatal("actual policy did not expire after capture")
	}
	select {
	case result := <-done:
		if result.err == nil || result.result.SchemaVersion != "" || !tracer.final.Load() {
			t.Fatal("final native policy predicate didn't reject expired captured policy", result.err, tracer.final.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native bounded read did not stop")
	}
}

func TestAgentToolNativePolicyChangeABAAndAbsentDefaultStopCheckedRead(t *testing.T) {
	for _, kind := range []string{"first-setting", "same-value-xmin", "level-change", "expired"} {
		t.Run(kind, func(t *testing.T) {
			f := outputActivityFixture(t, 4)
			b := f.f.native.private.base
			id := outputPublishedActivity(t, f, "public")
			if kind != "first-setting" {
				expiry := time.Now().Add(time.Hour)
				if kind == "expired" {
					expiry = time.Now().Add(3 * time.Second)
				}
				if _, e := b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, expiry)); e != nil {
					t.Fatal(e)
				}
			}
			o, _ := toolNativePlanner(t, f, id)
			d, call, e := agenttool.NewService(o.ToolPlan()).Check(b.ctx, f.f.native.access, o.Plan.Actions[0], f.gate)
			if e != nil || call == nil || d.Disposition != agenttool.Allow {
				t.Fatal("current policy prerequisite", e)
			}
			switch kind {
			case "first-setting":
				_, e = b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour)))
			case "same-value-xmin":
				b.exec(`WITH old AS(DELETE FROM agent_policy_settings WHERE agent_id=$1 AND family='AUTONOMY' RETURNING *) INSERT INTO agent_policy_settings(agent_id,owner_id,owner_type,family,schema_version,native_revision,settings,valid_from,expires_at,updated_at) SELECT agent_id,owner_id,owner_type,family,schema_version,native_revision,settings,valid_from,expires_at,updated_at FROM old`, b.personID)
			case "level-change":
				in := policyNativeInput(agentpolicysettings.Autonomy, 1, time.Now().Add(time.Hour))
				in.Settings = json.RawMessage(`{"level":"LEVEL_0_OBSERVE"}`)
				_, e = b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, in)
			case "expired":
				deadline := time.Now().Add(6 * time.Second)
				expired := false
				for time.Now().Before(deadline) {
					if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=expires_at FROM agent_policy_settings WHERE agent_id=$1 AND family='AUTONOMY'`, b.personID).Scan(&expired) != nil {
						t.Fatal("native clock unavailable")
					}
					if expired {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				if !expired {
					t.Fatal("actual native policy did not expire")
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			before := toolBusinessSnapshot(t, f)
			result, e := call.Read(b.ctx, f.f.native.access, f.gate)
			if e == nil || result.SchemaVersion != "" || len(result.Activities) != 0 {
				t.Fatal("stale checked read escaped", kind, e, result)
			}
			if before != toolBusinessSnapshot(t, f) {
				t.Fatal("rejected read wrote domain")
			}
		})
	}
}
func TestAgentToolNativeCurrentSocialDisabledDoesNotGrantOrBlockPublicPurpose(t *testing.T) {
	f := toolActivityFixture(t, 4)
	b := f.f.native.private.base
	id := outputPublishedActivity(t, f, "public")
	in := policyNativeInput(agentpolicysettings.Social, 0, time.Now().Add(time.Hour))
	in.Settings = json.RawMessage(`{"rules":[]}`)
	if _, e := b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Social, in); e != nil {
		t.Fatal(e)
	}
	o, _ := toolNativePlanner(t, f, id)
	d, result, e := agenttool.NewService(o.ToolPlan()).Read(b.ctx, f.f.native.access, o.Plan.Actions[0], f.gate)
	if e != nil || d.Disposition != agenttool.Allow || d.Purpose != "READ_CURRENT_PUBLIC_ACTIVITY" || len(result.Activities) != 1 {
		t.Fatal("unrelated social preference became permission/purpose", e, d)
	}
}

func TestAgentToolNativePolicyLockWaitRespectsCallerDeadlineAndNeverRetries(t *testing.T) {
	f := outputActivityFixture(t, 4)
	b := f.f.native.private.base
	id := outputPublishedActivity(t, f, "public")
	o, adapter := toolNativePlanner(t, f, id)
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(b.ctx)
	if _, e = blocker.Exec(b.ctx, `LOCK TABLE agent_policy_settings IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	beforeCalls := adapter.calls.Load()
	ctx, cancel := context.WithTimeout(b.ctx, 120*time.Millisecond)
	defer cancel()
	started := time.Now()
	d, call, e := agenttool.NewService(o.ToolPlan()).Check(ctx, f.f.native.access, o.Plan.Actions[0], f.gate)
	if e == nil || call != nil || d.Disposition != agenttool.Deny || time.Since(started) > 2*time.Second || ctx.Err() == nil {
		t.Fatal("blocked native policy wait escaped deadline", e, d, time.Since(started))
	}
	if adapter.calls.Load() != beforeCalls {
		t.Fatal("tool wait resent model request")
	}
	if e = blocker.Rollback(b.ctx); e != nil {
		t.Fatal(e)
	}
	retryAssertOriginalBudgets(t, f, 1)
}
