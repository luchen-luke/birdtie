package postgres

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"strings"
	"testing"
	"time"
)

func toolSandboxGoal(t *testing.T, f *egressFixture) (agentplanner.PreparedGoal, agenttool.SandboxProposal) {
	t.Helper()
	b := f.f.native.private.base
	operation := egressID(t, f)
	g, e := b.store.PrepareOwnReadonlyPlan(b.ctx, f.f.native.access, f.f.native.task.ID, operation)
	if e != nil {
		t.Fatal(e)
	}
	native, ok := g.(*nativePlannerGoal)
	if !ok {
		t.Fatal("not current native goal")
	}
	return g, agenttool.SandboxProposal{ActionID: egressID(t, f), LogicalOperationID: operation, TargetID: b.person.ID, Value: "本地沙箱待确认；不发消息或更改资料", ResourceVersion: native.source}
}
func TestAgentToolNativeSandboxOnlyConfirmExactVersionWithoutEffects(t *testing.T) {
	f := outputActivityFixture(t, 4)
	b := f.f.native.private.base
	if _, e := b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	goal, input := toolSandboxGoal(t, f)
	before := toolBusinessSnapshot(t, f)
	d, e := b.store.CheckOwnSandboxTool(b.ctx, f.f.native.access, goal, input, f.gate)
	if e != nil || d.Disposition != agenttool.Confirm || d.Tool != agenttool.SandboxWrite || d.SubjectID != b.person.ID || d.ArgumentsDigest != agenttool.Digest(input) || d.Purpose != "PREPARE_OWN_SANDBOX_APPROVAL" {
		t.Fatal("native sandbox confirmation lost", e, d)
	}
	raw, _ := json.Marshal(d)
	for _, forbidden := range []string{"approved", "executed", "effect_key", input.Value} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("false approval/effect/body in view")
		}
	}
	outputSaveWire(t, "tool-sandbox-real-confirm-no-effect", raw)
	input.Value = "同一操作另一具体版本仍需要重新确认"
	next, e := b.store.CheckOwnSandboxTool(b.ctx, f.f.native.access, goal, input, f.gate)
	if e != nil || next.Disposition != agenttool.Confirm || next.ArgumentsDigest == d.ArgumentsDigest || next.DecisionID == d.DecisionID {
		t.Fatal("changed body retained decision binding", e)
	}
	if before != toolBusinessSnapshot(t, f) {
		t.Fatal("sandbox confirmation mutated domain rows/xmin")
	}
	retryAssertOriginalBudgets(t, f, 0)
}
func TestAgentToolNativeSandboxDenyFirstRolesSourcesAndDefaultOFF(t *testing.T) {
	for _, kind := range []string{"observe-default", "off", "peer-session", "org-session", "business-session", "foreign-target", "changed-operation", "changed-resource", "task-aba", "session-revoked", "other-store", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := outputActivityFixture(t, 4)
			b := f.f.native.private.base
			if kind != "observe-default" {
				if _, e := b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour))); e != nil {
					t.Fatal(e)
				}
			}
			goal, input := toolSandboxGoal(t, f)
			access := f.f.native.access
			store := b.store
			ctx := b.ctx
			switch kind {
			case "off":
				_ = f.gate.Disable(agentfeature.Enrichment)
			case "peer-session":
				access = agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
			case "org-session":
				access = agentevent.Access{SessionDigest: f.f.native.private.org.SessionDigest}
			case "business-session":
				access = agentevent.Access{SessionDigest: f.f.native.private.biz.SessionDigest}
			case "foreign-target":
				input.TargetID = b.other.ID
			case "changed-operation":
				input.LogicalOperationID = egressID(t, f)
			case "changed-resource":
				input.ResourceVersion = strings.Repeat("1", 64)
			case "task-aba":
				b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, f.f.native.task.ID)
			case "session-revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, access.SessionDigest[:])
			case "other-store":
				store = New(b.pool, false)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := toolBusinessSnapshot(t, f)
			d, e := store.CheckOwnSandboxTool(ctx, access, goal, input, f.gate)
			if e == nil || d.Disposition != agenttool.Deny || d.ArgumentsDigest != "" {
				t.Fatal("denied native sandbox became confirmation/authority", kind, e, d)
			}
			if before != toolBusinessSnapshot(t, f) {
				t.Fatal("denial wrote business state")
			}
			if kind == "session-revoked" {
				outputAssertRevokedBudgetSQL(t, f, 0)
			} else {
				retryAssertOriginalBudgets(t, f, 0)
			}
		})
	}
}
