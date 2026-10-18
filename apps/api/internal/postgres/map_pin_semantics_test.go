package postgres

import (
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func TestMapIndependentCoincidentObjects(t *testing.T) {
	anchor := content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 31.2, Longitude: 121.4}
	leaves := []mapSpatialLeaf{
		{pin: content.MapContentPin{Kind: "PLACE_CONTENT", MarkerID: "place:a", PlaceID: "a", Anchor: anchor}},
		{pin: content.MapContentPin{Kind: "PLACE_CONTENT", MarkerID: "place:b", PlaceID: "b", Anchor: anchor}},
		{pin: content.MapContentPin{Kind: "MOMENT_ANCHOR", MarkerID: "anchor:a", MomentID: "m1", Anchor: anchor}},
		{pin: content.MapContentPin{Kind: "MOMENT_ANCHOR", MarkerID: "anchor:b", MomentID: "m1", Anchor: anchor}},
	}
	for _, zoom := range []float64{0, 5, 12, 19} {
		groups := mapSpatialGroups(leaves, zoom)
		if len(groups) != len(leaves) {
			t.Fatal("distinct stable objects merged", zoom)
		}
		for i, group := range groups {
			if len(group) != 1 || group[0] != i {
				t.Fatal("object identity or coordinate replaced", zoom, group)
			}
		}
	}
}

func TestMapIndependentLeafSourceIgnoresJoinOrder(t *testing.T) {
	a := &mapSpatialLeaf{pin: content.MapContentPin{MarkerID: "place:a"}, stamps: []string{"z-relation", "a-relation"}}
	b := &mapSpatialLeaf{pin: a.pin, stamps: []string{"a-relation", "z-relation"}}
	original := mapSpatialLeafSourceVersion("reader-a", a)
	if mapSpatialLeafSourceVersion("reader-a", b) != original {
		t.Fatal("SQL join order retired unchanged leaf")
	}
	b.stamps[1] = "changed-relation"
	if mapSpatialLeafSourceVersion("reader-a", b) == original || mapSpatialLeafSourceVersion("reader-b", a) == original {
		t.Fatal("changed facts or reader did not retire old source")
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

func TestCoincidentClusterIdentityAndSourceIsolation(t *testing.T) {
	anchor := content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 31.2, Longitude: 121.4}
	leaves := []mapSpatialLeaf{
		{pin: content.MapContentPin{MarkerID: "place:a", Kind: "PLACE_CONTENT", Name: "隔离地点", Anchor: anchor, SourceVersion: "fact-a"}},
		{pin: content.MapContentPin{MarkerID: "anchor:b", Kind: "MOMENT_ANCHOR", Name: "隔离动态", Anchor: anchor, SourceVersion: "fact-b"}},
		{pin: content.MapContentPin{MarkerID: "place:unrelated", Kind: "PLACE_CONTENT", Anchor: content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 31.3, Longitude: 121.5}, SourceVersion: "other"}},
	}
	ids := []int{0, 1}
	baseID, baseSource := mapClusterID("reader-a", leaves, ids), mapClusterSourceVersion("reader-a", leaves, ids)
	if mapClusterID("reader-a", leaves, []int{1, 0}) != baseID || mapClusterSourceVersion("reader-a", leaves, []int{1, 0}) != baseSource {
		t.Fatal("input ordering must not replace a coincident group's identity or source")
	}
	leaves[2].pin.SourceVersion = "unrelated-updated"
	if mapClusterID("reader-a", leaves, ids) != baseID || mapClusterSourceVersion("reader-a", leaves, ids) != baseSource {
		t.Fatal("an unrelated viewport group must not retire this group's source")
	}
	if mapClusterID("reader-b", leaves, ids) == baseID || mapClusterSourceVersion("reader-b", leaves, ids) == baseSource {
		t.Fatal("reader authority must remain fenced")
	}
	leaves[1].pin.SourceVersion = "changed-member-authority"
	if mapClusterID("reader-a", leaves, ids) != baseID || mapClusterSourceVersion("reader-a", leaves, ids) == baseSource {
		t.Fatal("changed member facts must retire its source without inventing a new position")
	}
	if mapClusterID("reader-a", leaves, []int{0}) == baseID {
		t.Fatal("removed members must change group identity")
	}
	leaves[0].pin.Anchor.Latitude += .01
	leaves[1].pin.Anchor = leaves[0].pin.Anchor
	if mapClusterID("reader-a", leaves, ids) == baseID {
		t.Fatal("a changed true anchor must change group identity")
	}
}
