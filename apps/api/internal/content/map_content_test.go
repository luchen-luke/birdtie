package content

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestMapContentBoundsAndSourceScopeAreClosed(t *testing.T) {
	if !(MapContentQuery{CityID: "aberdeen-gb", Bounds: MapContentBounds{-2.4, 57, -1.9, 57.3}}).Valid() {
		t.Fatal("valid query rejected")
	}
	if (MapContentBounds{math.NaN(), 0, 1, 1}).Valid() || !(MapContentBounds{179, 0, -179, 1}).Valid() {
		t.Fatal("nonfinite/reversed bounds accepted")
	}
	if MapContentVisibility("PUBLIC") || !MapContentVisibility("CIVU_PUBLIC_MOMENT") {
		t.Fatal("legacy grant confused with native publication")
	}
}

func TestMapContent171AnchorIdentityAndPermissionCount(t *testing.T) {
	now := time.Now().UTC()
	id := "17100000-0000-4000-8000-000000000001"
	mid := "17100000-0000-4000-8000-000000000002"
	viewer := "17100000-0000-4000-8000-000000000003"
	h := strings.Repeat("a", 64)
	pin := MapContentPin{MarkerID: "anchor:" + id, Kind: "MOMENT_ANCHOR", EntityID: mid, AnchorID: id, MomentID: mid, EntryID: mid, Name: "动态", CategoryCode: "moment", Anchor: MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 57, Longitude: -2}, AnchorCount: 1, MomentCount: 1, VisibilitySources: []string{"SELF_PRIVATE"}, SourceVersion: h, Moments: []MapContentMomentSelector{{ID: mid, Revision: 1, VisibilitySource: "SELF_PRIVATE", DetailPath: "/v1/me/map-content/moments/" + mid}}}
	page := MapContentPage{SchemaVersion: MapContentSchema, Scope: "MAP_CONTENT", ViewerAccountID: viewer, Bounds: MapContentBounds{West: -3, South: 56, East: -1, North: 58}, Zoom: 15, FilterScope: "DEFAULT_CONTENT", TotalContentCount: 1, TotalAnchorCount: 1, Pins: []MapContentPin{pin}, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), SourceVersion: h}
	if !page.Valid(now) {
		t.Fatal("no-place anchor rejected")
	}
	page.Pins[0].EntryID = id
	if page.Valid(now) {
		t.Fatal("anchor routed to wrong child")
	}
	page.Pins[0] = pin
	page.Pins[0].MomentCount = 0
	if page.Valid(now) {
		t.Fatal("empty content admitted")
	}
	page.Pins[0] = pin
	page.Pins[0].VisibilitySources = []string{"CIVU_PUBLIC_CATALOG"}
	if page.Valid(now) {
		t.Fatal("catalogue visibility stood in for private content permission")
	}
}

func TestMapContentLazyDisplayDescriptorBindsExactOriginal(t *testing.T) {
	now := time.Now().UTC()
	mid, id, owner := "13000000-0000-4000-8000-000000000001", "13000000-0000-4000-8000-000000000002", "13000000-0000-4000-8000-000000000003"
	original := strings.Repeat("a", 64)
	source := MomentControlHash([]any{original, "display1280-v1"})
	v := MapContentDisplayDescriptor{SchemaVersion: MapContentDisplaySchema, ViewerAccountID: owner, MomentID: mid, OriginalSourceVersion: original, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), Media: MapContentMedia{Kind: "IMAGE", ID: id, Revision: 1, MIMEType: "image/jpeg", Width: 960, Height: 1280, ByteCount: 1234, SHA256: strings.Repeat("b", 64), SourceVersion: source, ContentPath: "/v1/me/map-content/moments/" + mid + "/media/" + id + "/content?sourceVersion=" + source + "&variant=display"}}
	if !v.Valid(now) {
		t.Fatal("valid display descriptor rejected")
	}
	for _, mutate := range []func(*MapContentDisplayDescriptor){
		func(v *MapContentDisplayDescriptor) { v.OriginalSourceVersion = strings.Repeat("c", 64) },
		func(v *MapContentDisplayDescriptor) { v.MomentID = owner },
		func(v *MapContentDisplayDescriptor) { v.Media.ID = owner },
		func(v *MapContentDisplayDescriptor) { v.Media.Width = 1281 },
		func(v *MapContentDisplayDescriptor) { v.Media.ByteCount = 256*1024 + 1 },
		func(v *MapContentDisplayDescriptor) { v.Media.DisplayDescriptorPath = "recursive" },
		func(v *MapContentDisplayDescriptor) { v.ValidUntil = now },
		func(v *MapContentDisplayDescriptor) { v.Media.ContentPath += "&unexpected=1" },
	} {
		bad := v
		mutate(&bad)
		if bad.Valid(now) {
			t.Fatal("unbound descriptor accepted")
		}
	}
}
