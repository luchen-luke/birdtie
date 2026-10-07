package postgres

import (
	"encoding/json"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"strings"
	"testing"
)

func xActionProposal(action, op, target, source string) agenttool.SandboxProposal {
	return agenttool.SandboxProposal{ActionID: action, LogicalOperationID: op, TargetID: target, Value: "合成本人独立沙箱；不联系真实用户", ResourceVersion: source}
}
func TestAgentActionNativeAuditRedactedAndJSONFlagsCannotApprove(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	raw, _ := json.Marshal(x.p)
	var fake map[string]any
	if json.Unmarshal(raw, &fake) != nil {
		t.Fatal("fixture")
	}
	fake["confirmed"] = true
	fake["approved"] = true
	fake["state"] = aa.Approved
	if d, h, e := b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate); e == nil || d.ID != "" || h != nil {
		t.Fatal("JSON view conferred authority")
	}
	actionApprove(t, x)
	d, h := actionCommitNative(t, x)
	_, cl, e := h.Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cl.Execute(b.ctx, f.f.native.access, f.gate); e != nil {
		t.Fatal(e)
	}
	var audits string
	if e = b.pool.QueryRow(b.ctx, `SELECT jsonb_agg(jsonb_build_object('actor',actor_account_id,'action',action,'resource_type',resource_type,'resource_id',resource_id,'decision',decision,'purpose',purpose,'request_id',request_id,'target_id',target_resource_id) ORDER BY id)::text FROM audit_events WHERE actor_account_id=$1 AND purpose='OWN_SANDBOX_ACTION'`, b.person.ID).Scan(&audits); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{x.p.Proposal.Value, x.p.Binding.AuthorityVersion, x.p.Binding.PolicyVersion, x.p.Binding.SourceVersion, "token_sha256", "hidden_reasoning", "session_digest", "payload", "private_chat"} {
		if strings.Contains(audits, v) {
			t.Fatal("raw private content/permission proof in audit", v)
		}
	}
	if !strings.Contains(audits, "sandbox_dispatch") || !strings.Contains(audits, "sandbox_reconcile") || !strings.Contains(audits, d.ID) {
		t.Fatal("actual decision trace missing")
	}
	outputSaveWire(t, "sandbox-native-redacted-audit", []byte(audits))
}
