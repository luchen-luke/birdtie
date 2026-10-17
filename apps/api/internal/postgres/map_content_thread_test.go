package postgres

import (
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

// Uses the existing marked disposable PG fixture only when explicitly wired.
// A sibling need not have a map anchor to remain part of its real thread.
func TestMapContentThreadNativeOrderedReadAndRetiredPermission(t *testing.T) {
	f := savedPageNative(t)
	in := threadNativeInput(t, f)
	in.Items[1].Input.PlaceID = ""
	in.Items[1].Input.CityID = ""
	in.Items[1].Input.LocationPrecision = "none"
	r, err := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, in))
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.store.ReadHumanMapContentThread(f.ctx, f.digest, f.actor, r.Members[1].MomentID)
	if err != nil || len(v.Entries) != 2 || v.Entries[1].Detail == nil || v.Entries[1].Detail.Body != in.Items[1].Input.Body {
		t.Fatal("authoritative sibling sequence", err)
	}
	if err = v.ResponseProof.RevalidateHumanControl(f.ctx); err != nil {
		t.Fatal("initial response proof", err)
	}
	before := v.ResponseProof
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: r.Members[0].MomentID, ExpectedRevision: 2})
	v, err = f.store.ReadHumanMapContentThread(f.ctx, f.digest, f.actor, r.Members[1].MomentID)
	if err != nil || len(v.Entries) != 2 || v.Entries[0].Detail != nil || v.Entries[0].Status != "UNAVAILABLE" || v.Entries[1].Detail == nil {
		t.Fatal("withdrawn sibling was exposed or collapsed", err)
	}
	if err = before.RevalidateHumanControl(f.ctx); err == nil {
		t.Fatal("stale authority proof survived withdrawal")
	}
}

func TestMapContentThreadNativeCrossViewerBlockAndSessionRetirement(t *testing.T) {
	f := savedPageNative(t)
	other := interactionNativeActor(t, f)
	in := threadNativeInput(t, f)
	r, err := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, in))
	if err != nil {
		t.Fatal(err)
	}
	seed := r.Members[1].MomentID
	v, err := f.store.ReadHumanMapContentThread(f.ctx, other.digest, other.actor, seed)
	if err != nil || len(v.Entries) != 2 || v.Entries[0].Detail == nil || v.Entries[1].Detail == nil {
		t.Fatal("current public thread unavailable to legitimate second viewer", err)
	}
	if err = v.ResponseProof.RevalidateHumanControl(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, other.actor.ID, f.actor.ID)
	if _, err = f.store.ReadHumanMapContentThread(f.ctx, other.digest, other.actor, seed); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("blocked viewer discovered authoritative thread", err)
	}
	if err = v.ResponseProof.RevalidateHumanControl(f.ctx); err == nil {
		t.Fatal("old response crossed account block")
	}
	f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, other.actor.ID, f.actor.ID)
	v, err = f.store.ReadHumanMapContentThread(f.ctx, other.digest, other.actor, seed)
	if err != nil {
		t.Fatal("unblocked current read", err)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, other.session)
	if _, err = f.store.ReadHumanMapContentThread(f.ctx, other.digest, other.actor, seed); err == nil {
		t.Fatal("revoked session read thread")
	}
	if err = v.ResponseProof.RevalidateHumanControl(f.ctx); err == nil {
		t.Fatal("old response crossed session revocation")
	}
}

func TestMapContentThreadNativeUsesEditedDisplayOrder(t *testing.T) {
	f := savedPageNative(t)
	in := threadNativeInput(t, f)
	r, err := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, in))
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.ReadHumanMapContentThread(f.ctx, f.digest, f.actor, r.Members[1].MomentID)
	if err != nil {
		t.Fatal(err)
	}
	noRestore, empty := false, ""
	removed := []content.MomentThreadRemoval{}
	items := []content.MomentThreadItem{}
	for _, i := range []int{1, 0} {
		items = append(items, content.MomentThreadItem{MomentID: r.Members[i].MomentID, ExpectedRevision: r.Members[i].AppliedRevision, Restore: &noRestore, Input: in.Items[i].Input, Media: nil})
	}
	edit := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", ThreadID: r.ThreadID, BaseMomentID: &empty, LocalScope: in.LocalScope, Audience: "PUBLIC", ExpectedVersion: r.AppliedVersion, Items: items, RemovedItems: &removed}
	if _, err = f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, edit)); err != nil {
		t.Fatal("authoritative reorder", err)
	}
	v, err := f.store.ReadHumanMapContentThread(f.ctx, f.digest, f.actor, r.Members[1].MomentID)
	if err != nil || len(v.Entries) != 2 || v.Entries[0].Detail == nil || v.Entries[1].Detail == nil || v.Entries[0].Detail.ID != r.Members[1].MomentID || v.Entries[1].Detail.ID != r.Members[0].MomentID {
		t.Fatal("map reader ignored authoritative edited display order", err)
	}
	if err = before.ResponseProof.RevalidateHumanControl(f.ctx); err == nil {
		t.Fatal("old order proof survived edit")
	}
}
