package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
)

func TestMapContentThreadNativeSingletonContainerNotSerial(t *testing.T) {
	f := savedPageNative(t)
	original := threadNativeInput(t, f)
	empty := ""
	removed := []content.MomentThreadRemoval{}
	restore := false
	original.Items[0].Restore = &restore
	original.Items[1].Restore = &restore
	single := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", BaseMomentID: &empty, LocalScope: original.LocalScope, Audience: "PUBLIC", Items: original.Items[:1], RemovedItems: &removed}
	created, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, single))
	if e != nil || len(created.Members) != 1 {
		t.Fatal("explicit single-entry native container", e)
	}
	seed := created.Members[0].MomentID
	one, e := f.store.ReadHumanMapContentThread(f.ctx, f.digest, f.actor, seed)
	if e != nil || one.Status != "NOT_A_THREAD" || one.ThreadID != "" || len(one.Entries) != 0 {
		t.Fatal("native one-entry container mislabeled", e)
	}
	if e = one.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal(e)
	}
	next := original.Items[1]
	first := original.Items[0]
	first.MomentID = seed
	first.ExpectedRevision = created.Members[0].AppliedRevision
	first.Restore = &restore
	first.Media = nil
	upgrade := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", ThreadID: created.ThreadID, BaseMomentID: &empty, ExpectedVersion: created.AppliedVersion, LocalScope: original.LocalScope, Audience: "PUBLIC", Items: []content.MomentThreadItem{first, next}, RemovedItems: &removed}
	changed, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, upgrade))
	if e != nil || len(changed.Members) != 2 {
		t.Fatal("explicit second entry", e)
	}
	two, e := f.store.ReadHumanMapContentThread(f.ctx, f.digest, f.actor, seed)
	if e != nil || two.Status != "AVAILABLE" || two.ThreadID != created.ThreadID || len(two.Entries) != 2 {
		t.Fatal("explicit native grouping lost", e)
	}
	if one.ResponseProof.RevalidateHumanControl(f.ctx) == nil {
		t.Fatal("late singleton proof survived explicit grouping change")
	}
}
