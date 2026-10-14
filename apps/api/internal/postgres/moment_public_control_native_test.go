package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
)

func TestMomentPublicControlNativePublishEditDeleteOriginalFacts(t *testing.T) {
	f := savedPageNative(t)
	_, place := f.bookmark(t)
	create := mc144Input(t, f)
	create.Input.PlaceID = place
	create.Input.LocationPrecision = "place"
	draft, _ := mc144Write(t, f, create)
	value := *create.Input
	publish := content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: &value}
	published, approved := mc144Write(t, f, publish)
	if published.AppliedRevision != 2 {
		t.Fatal("publish revision", published)
	}
	replay, err := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, approved)
	if err != nil || replay.OperationID != published.OperationID || replay.AppliedRevision != 2 {
		t.Fatal("one original publication", err)
	}
	changed := value
	changed.Body = "本人更新后的公开正文"
	edit := content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: draft.MomentID, ExpectedRevision: 2, Input: &changed}
	edited, _ := mc144Write(t, f, edit)
	if edited.AppliedRevision != 3 {
		t.Fatal("edit revision")
	}
	withdraw := content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: draft.MomentID, ExpectedRevision: 3}
	deleted, _ := mc144Write(t, f, withdraw)
	if deleted.AppliedRevision != 4 {
		t.Fatal("delete revision")
	}
	original, err := f.store.ReadMomentControlOperation(f.ctx, f.digest, f.actor, publish.OperationID)
	if err != nil || original.Action != "PUBLISH" || original.AppliedRevision != 2 {
		t.Fatal("later deletion erased original publish fact", err)
	}
	var status, visibility string
	var count int
	if err = f.pool.QueryRow(f.ctx, `SELECT status,visibility FROM moments WHERE id=$1`, draft.MomentID).Scan(&status, &visibility); err != nil || status != "withdrawn" || visibility != "private" {
		t.Fatal("canonical deletion", err)
	}
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM human_moment_control_operations WHERE actor_account_id=$1`, f.actor.ID).Scan(&count); err != nil || count != 4 {
		t.Fatal("replay created another effect", count, err)
	}
}
