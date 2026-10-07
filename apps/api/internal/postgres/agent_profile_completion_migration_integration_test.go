package postgres

import (
	"os"
	"reflect"
	"testing"

	apc "github.com/birdtie/birdtie/apps/api/internal/agentprofilecompletion"
)

func completionRejectUsedDown(t *testing.T, f *completionNativeFixture, down []byte) {
	t.Helper()
	b := f.f.base
	connection, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Release()
	_, rejected := connection.Exec(b.ctx, string(down))
	if _, e = connection.Exec(b.ctx, `ROLLBACK`); e != nil {
		t.Fatal("used-down same-connection rollback", e)
	}
	if rejected == nil {
		t.Fatal("used down erased completion history")
	}
}

func TestProfileMemoryCompletionNativeMigrationHistoryAndNoRenewal(t *testing.T) {
	ownedMigrationDatabase(t)
	f := completionNative(t, "hiking", 3600000000000)
	b := f.f.base
	p := f.preview(t)
	down, e := os.ReadFile("../../migrations/093_agent_profile_memory_completion.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	before := completionSnapshot(t, b.pool, b.ctx, "used-down-before")
	completionRejectUsedDown(t, f, down)
	requireCompletionPairsEqual(t, before, completionSnapshot(t, b.pool, b.ctx, "used-down-after"))
	if _, e = b.pool.Exec(b.ctx, `UPDATE agent_profile_completion_previews SET expires_at=expires_at+interval '1 second' WHERE id=$1`, p.ID); e == nil {
		t.Fatal("preview renewal allowed")
	}
	if _, e = b.pool.Exec(b.ctx, `DELETE FROM agent_profile_completion_previews WHERE id=$1`, p.ID); e == nil {
		t.Fatal("history erased without parent deletion")
	}
	r, e := b.store.AcceptOwnProfileCompletion(b.ctx, f.f.owner, p.ID, apc.AcceptInput{PlanDigest: p.PlanDigest})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE agent_profile_completion_previews SET committed_at=committed_at WHERE id=$1`, p.ID); e == nil {
		t.Fatal("terminal receipt mutable")
	}
	completionRejectUsedDown(t, f, down)
	current, e := b.store.ReadOwnProfileCompletion(b.ctx, f.f.owner, p.ID)
	if e != nil || current.ResultProfileVersion == nil || !reflect.DeepEqual(current.ResultProfileVersion, r.ResultProfileVersion) {
		t.Fatal("history rejection lost receipt", e)
	}
}
func TestProfileMemoryCompletionNativeMigrationUnusedDownReapply(t *testing.T) {
	ownedMigrationDatabase(t)
	f := completionNative(t, "hiking", 3600000000000)
	b := f.f.base
	down, e := os.ReadFile("../../migrations/093_agent_profile_memory_completion.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile("../../migrations/093_agent_profile_memory_completion.sql")
	if e != nil {
		t.Fatal(e)
	}
	before := completionSnapshot(t, b.pool, b.ctx, "unused-before")
	if before["agent_profile_completion_previews"] != "[]" {
		t.Fatal("unused schema unexpectedly holds history")
	}
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal(e)
	}
	downRows := initialCaptureSnapshot(t, b.pool, b.ctx, "completion-unused-down")
	if len(downRows)+1 != len(before) {
		t.Fatal("down removed unrelated table")
	}
	for name, v := range downRows {
		if before[name] != v {
			t.Fatal("down changed old rows/xmin", name)
		}
	}
	r, e := b.store.ReadOwnProfileCompletionSuggestions(b.ctx, f.f.owner)
	if e == nil || !reflect.DeepEqual(r, apc.Suggestions{}) {
		t.Fatal("schema092 manufactured new completion capability")
	}
	if _, e = b.pool.Exec(b.ctx, string(up)); e != nil {
		t.Fatal(e)
	}
	requireCompletionPairsEqual(t, before, completionSnapshot(t, b.pool, b.ctx, "unused-reapply"))
}
