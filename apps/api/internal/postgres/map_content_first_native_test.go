package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
	"time"
)

// All source objects live in the existing disposable owned database fixture.
func TestContentFirstNativeIndependentMomentsAndRouteGap(t *testing.T) {
	f := savedPageNative(t)
	_, place := f.bookmark(t)
	in := threadNativeInput(t, f)
	located := in.Items[0]
	located.Input.PlaceID = place
	located.Input.LocationPrecision = "place"
	in.Items = []content.MomentThreadItem{located, in.Items[1], located, located}
	published, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, in))
	if e != nil {
		t.Fatal("publish real ordered test thread", e)
	}
	q := content.MapContentQuery{CityID: "aberdeen-gb", Zoom: 14, Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}}
	page, e := f.store.ReadHumanMapContent(f.ctx, f.digest, f.actor, q)
	if e != nil || !page.Valid(time.Now()) {
		t.Fatal("map projection", e)
	}
	pins := map[string]content.MapContentPin{}
	for _, p := range page.Pins {
		if p.ThreadID == published.ThreadID {
			pins[p.MomentID] = p
		}
	}
	if len(pins) != 3 {
		t.Fatalf("same Place must retain three original Moments, got %d", len(pins))
	}
	first, third, fourth := pins[published.Members[0].MomentID], pins[published.Members[2].MomentID], pins[published.Members[3].MomentID]
	if first.MarkerID == third.MarkerID || third.MarkerID == fourth.MarkerID {
		t.Fatal("coincident content identity merged")
	}
	for _, p := range pins {
		if p.Kind != "MOMENT_ANCHOR" || p.PlaceID != place || p.ContentTime == nil || p.TimeSource != "PUBLISHED" {
			t.Fatal("lost native content/time/place", p)
		}
	}
	if len(first.NextMarkerIDs) != 0 || len(third.NextMarkerIDs) != 1 || third.NextMarkerIDs[0] != fourth.MarkerID || third.ThreadPosition != 2 || fourth.ThreadPosition != 3 {
		t.Fatal("route jumped unlocated entry or renumbered it")
	}
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: fourth.MomentID, ExpectedRevision: 2})
	fresh, e := f.store.ReadHumanMapContent(f.ctx, f.digest, f.actor, q)
	if e != nil {
		t.Fatal("withdrawn endpoint read", e)
	}
	for _, p := range fresh.Pins {
		if p.MomentID == fourth.MomentID || p.MomentID == third.MomentID && len(p.NextMarkerIDs) > 0 {
			t.Fatal("withdrawn node or edge revived")
		}
	}
	if page.ResponseProof.RevalidateHumanControl(f.ctx) == nil {
		t.Fatal("old map source proof survived endpoint withdrawal")
	}
}
