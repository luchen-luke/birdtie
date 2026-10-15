package postgres

import (
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestMomentCityPublicControlNativeOriginalFactsAndCurrentContext(t *testing.T) {
	f := savedPageNative(t)
	create := mc144Input(t, f)
	draft, _ := mc144Write(t, f, create)
	value := *create.Input
	publish := content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: &value}
	published, approved := mc144Write(t, f, publish)
	if published.AppliedRevision != 2 {
		t.Fatal("city public revision", published)
	}
	if replay, err := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, approved); err != nil || replay.OperationID != published.OperationID || replay.AppliedRevision != 2 {
		t.Fatal("city original publication replay", err)
	}
	access := content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}
	r, err := f.store.CapturePublicMomentWithContext(f.ctx, access, draft.MomentID, true, true)
	if err != nil || r.View.SchemaVersion != content.PublicMomentContextSchemaVersion || r.View.LocationPrecision != "city" || r.View.PlaceID != "" || r.View.PlaceName != "" || r.View.Photo != nil || r.View.CityName == "" || r.View.Body != value.Body {
		t.Fatal("actual v4 city text without invented Place or photo", err)
	}
	if _, err = f.store.FinalizePublicMomentWithContext(f.ctx, access, r); err != nil {
		t.Fatal("original native city snapshot finalization", err)
	}
	if _, err = f.store.GetPublicMoment(f.ctx, access, draft.MomentID); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("closed Place-only legacy reader changed", err)
	}
	changed := value
	changed.Body = "本人更新的城市公开正文"
	edit := content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: draft.MomentID, ExpectedRevision: 2, Input: &changed}
	stale, _ := mc144Preview(t, f, edit)
	f.exec(t, `UPDATE cities SET name=name WHERE id=$1`, value.CityID)
	if _, err = f.store.CommitMomentControl(f.ctx, f.digest, f.actor, stale); !errors.Is(err, identity.ErrControlChanged) {
		t.Fatal("stale city approval wrote after city epoch changed", err)
	}
	if _, err = f.store.FinalizePublicMomentWithContext(f.ctx, access, r); !errors.Is(err, identity.ErrControlChanged) {
		t.Fatal("stale city response reused its old source", err)
	}
	edited, _ := mc144Write(t, f, edit)
	if edited.AppliedRevision != 3 {
		t.Fatal("city public edit revision", edited)
	}
	guest, err := f.store.CapturePublicMomentWithContext(f.ctx, content.PublicMomentAccess{}, draft.MomentID, false, true)
	if err != nil || guest.View.Body != changed.Body || guest.View.AuthorID != nil || guest.View.AuthorPublicProfile != nil {
		t.Fatal("current city text/explicit author projection", err)
	}
	f.exec(t, `UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, value.CityID)
	if _, err = f.store.CapturePublicMomentWithContext(f.ctx, access, draft.MomentID, false, true); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("expired city released public content", err)
	}
	f.exec(t, `UPDATE cities SET expires_at=NULL WHERE id=$1`, value.CityID)
	withdraw := content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: draft.MomentID, ExpectedRevision: 3}
	deleted, _ := mc144Write(t, f, withdraw)
	if deleted.AppliedRevision != 4 {
		t.Fatal("city withdrawal revision", deleted)
	}
	if _, err = f.store.CapturePublicMomentWithContext(f.ctx, access, draft.MomentID, true, true); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("withdrawn city text remained public", err)
	}
	original, err := f.store.ReadMomentControlOperation(f.ctx, f.digest, f.actor, publish.OperationID)
	if err != nil || original.AppliedRevision != 2 || original.Action != "PUBLISH" {
		t.Fatal("later withdrawal erased original city publication", err)
	}
	var count int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM human_moment_control_operations WHERE actor_account_id=$1`, f.actor.ID).Scan(&count); err != nil || count != 4 {
		t.Fatal("replay or changed city created another operation effect", count, err)
	}
	// Another live city Moment demonstrates that the original Session proof is
	// retained by the finalizer, rather than accepting a later auth fallback.
	second, _ := mc144Write(t, f, mc144Input(t, f))
	secondPublish := content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: second.MomentID, ExpectedRevision: 1, Input: &value}
	mc144Write(t, f, secondPublish)
	current, err := f.store.CapturePublicMomentWithContext(f.ctx, access, second.MomentID, false, true)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if _, err = f.store.FinalizePublicMomentWithContext(f.ctx, access, current); err == nil {
		t.Fatal("revoked original Session released city content")
	}
}
