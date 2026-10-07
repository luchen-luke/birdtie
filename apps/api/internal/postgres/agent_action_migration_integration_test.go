package postgres

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentActionNativeSQLTamperCannotRewriteBindingsOrInventSuccess(t *testing.T) {
	for _, kind := range []string{"binding", "payload", "consume-alone", "grant-expiry", "grant-purpose", "success", "effect"} {
		t.Run(kind, func(t *testing.T) {
			x := actionNativeFixture(t)
			f := x.f
			b := f.f.native.private.base
			actionApprove(t, x)
			tx, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			var q string
			var args []any
			switch kind {
			case "binding":
				q = `UPDATE agent_action_approvals SET binding_digest=repeat('0',64) WHERE id=$1`
				args = []any{x.p.Binding.ApprovalID}
			case "payload":
				q = `UPDATE agent_action_approvals SET payload_canonical='{}' WHERE id=$1`
				args = []any{x.p.Binding.ApprovalID}
			case "consume-alone":
				q = `UPDATE agent_action_approvals SET consumed_at=clock_timestamp() WHERE id=$1`
				args = []any{x.p.Binding.ApprovalID}
			case "grant-expiry":
				q = `UPDATE consent_grants SET expires_at=expires_at+interval '1 hour' WHERE id=$1`
				args = []any{x.p.Binding.GrantID}
			case "grant-purpose":
				q = `UPDATE consent_grants SET purpose='TASK_CONTEXT_READ' WHERE id=$1`
				args = []any{x.p.Binding.GrantID}
			case "success":
				q = `UPDATE agent_action_dispatches SET state='SUCCEEDED',effect_id=gen_random_uuid(),applied_at=clock_timestamp() WHERE approval_id=$1`
				d, _ := actionCommitNative(t, x)
				q = `UPDATE agent_action_dispatches SET state='SUCCEEDED',effect_id=gen_random_uuid(),applied_at=clock_timestamp() WHERE id=$1`
				args = []any{d.ID}
			case "effect":
				q = `INSERT INTO agent_sandbox_writes(id,dispatch_id,owner_id,target_id,effect_key,value) VALUES(gen_random_uuid(),$1,$2,$2,repeat('0',64),'fake')`
				d, _ := actionCommitNative(t, x)
				args = []any{d.ID, b.person.ID}
			}
			_, e = tx.Exec(b.ctx, q, args...)
			if e == nil {
				e = tx.Commit(b.ctx)
			}
			if e == nil {
				t.Fatal("SQL tamper accepted", kind)
			}
			if _, w := actionRows(t, x); w != 0 {
				t.Fatal("tamper created actual effect")
			}
		})
	}
}

func TestAgentActionNative097CompatibleRoundtripUsedHistoryRefusesDown(t *testing.T) {
	ownedMigrationDatabase(t)
	x := actionNativeFixture(t)
	b := x.f.f.native.private.base
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "097_agent_action_approval_dispatch.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(raw)); e == nil {
		t.Fatal("used immutable approval history silently dropped")
	}
	// Explicit rollback of the intentionally refused transaction, no table or
	// old record is removed. The runner independently verifies empty down/up.
	if _, e = b.pool.Exec(b.ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	var present bool
	if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_action_approvals WHERE id=$1)`, x.p.Binding.ApprovalID).Scan(&present); e != nil || !present {
		t.Fatal("failed down lost original approval", e)
	}
}
