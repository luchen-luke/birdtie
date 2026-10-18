package postgres

import (
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func TestMapPinRepresentedMomentSemantics(t *testing.T) {
	leaf := func(kind, id, mid string, lat, lon float64) *mapSpatialLeaf {
		return &mapSpatialLeaf{pin: content.MapContentPin{
			Kind: kind, MarkerID: id, MomentID: mid,
			Anchor: content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: lat, Longitude: lon},
		}, moments: map[string]content.MapContentMomentSelector{mid: {ID: mid}}}
	}
	for _, sameCoordinate := range []bool{true, false} {
		p := leaf("PLACE_CONTENT", "place:a", "m1", 31.2, 121.4)
		a := leaf("MOMENT_ANCHOR", "anchor:a", "m1", 31.3, 121.5)
		if sameCoordinate {
			a.pin.Anchor = p.pin.Anchor
		}
		b := leaf("MOMENT_ANCHOR", "anchor:b", "independent", 31.2, 121.4)
		c := leaf("MOMENT_ANCHOR", "anchor:c", "independent", 31.25, 121.45)
		p2 := leaf("PLACE_CONTENT", "place:b", "m1", 31.4, 121.6)
		rows := map[string]*mapSpatialLeaf{"p": p, "a": a, "b": b, "c": c, "p2": p2}
		suppressRepresentedMapAnchors(rows)
		if len(rows) != 4 || rows["a"] != nil || rows["p"] != p || rows["p2"] != p2 || rows["b"] != b || rows["c"] != c {
			t.Fatalf("sameCoordinate=%v: linked duplication or independent anchors changed: %#v", sameCoordinate, rows)
		}
	}
	// Hidden/out-of-viewport Places are absent from the authorized leaf set.
	a := leaf("MOMENT_ANCHOR", "anchor:a", "m1", 31.3, 121.5)
	rows := map[string]*mapSpatialLeaf{"a": a}
	suppressRepresentedMapAnchors(rows)
	if rows["a"] != a {
		t.Fatal("raw association must not suppress current independent anchor")
	}
	p := leaf("PLACE_CONTENT", "invalid", "m1", 0, 0)
	rows["p"] = p
	suppressRepresentedMapAnchors(rows)
	if rows["a"] != a {
		t.Fatal("invalid Place cannot represent anchor")
	}
}

func TestMapClusterCategoryUsesObjectComposition(t *testing.T) {
	leaves := []mapSpatialLeaf{
		{pin: content.MapContentPin{Kind: "PLACE_CONTENT"}},
		{pin: content.MapContentPin{Kind: "PLACE_CONTENT"}},
		{pin: content.MapContentPin{Kind: "MOMENT_ANCHOR"}},
		{pin: content.MapContentPin{Kind: "MOMENT_ANCHOR"}},
	}
	for _, tc := range []struct {
		members        []int
		name, category string
	}{
		{[]int{0, 1}, "地点", "place_cluster"},
		{[]int{2, 3}, "动态", "moment_cluster"},
		{[]int{0, 2}, "地图内容", "mixed_cluster"},
	} {
		name, category := mapClusterCategory(leaves, tc.members)
		if name != tc.name || category != tc.category {
			t.Fatalf("members=%v got %s/%s", tc.members, name, category)
		}
	}
}
