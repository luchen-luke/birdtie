package postgres

import (
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
