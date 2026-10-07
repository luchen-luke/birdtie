package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"reflect"
	"testing"
	"time"
)

// Real old native CONFIRM must have a durable original-domain continuation.
// The first run executes the existing Session/Task/Policy path, then fails at
// the absent persistence capability rather than at an undefined Go symbol.
func TestAgentActionNativeOriginalConfirmRequiresPersistentApproval(t *testing.T) {
	f := outputActivityFixture(t, 4)
	b := f.f.native.private.base
	if _, e := b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	g, p := toolSandboxGoal(t, f)
	d, e := b.store.CheckOwnSandboxTool(b.ctx, f.f.native.access, g, p, f.gate)
	if e != nil || d.Disposition != agenttool.Confirm {
		t.Fatal("original actual native confirmation prerequisite", e, d)
	}
	if !reflect.ValueOf(b.store).MethodByName("PreviewOwnSandboxAction").IsValid() {
		t.Fatal("actual original CONFIRM has no persistent exact-version approval/dispatch/sandbox-effect continuation")
	}
	var present bool
	if e = b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_action_approvals') IS NOT NULL`).Scan(&present); e != nil || !present {
		t.Fatal("actual approval persistence absent", e)
	}
}
