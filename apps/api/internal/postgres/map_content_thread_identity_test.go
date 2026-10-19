package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
)

func TestMapContentRouteEndpointPreservesIndependentPoints(t *testing.T) {
	leaf := func(id, mid, place string, lat float64) mapSpatialLeaf {
		return mapSpatialLeaf{pin: content.MapContentPin{MarkerID: id, MomentID: mid, PlaceID: place, Anchor: content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: lat, Longitude: 2}}}
	}
	leaves := []mapSpatialLeaf{leaf("anchor:z", "moment-a", "place-a", 1), leaf("anchor:a", "moment-a", "place-a", 1), leaf("anchor:b", "moment-a", "place-b", 3), leaf("anchor:c", "moment-b", "place-a", 1), leaf("anchor:d", "moment-a", "", 1)}
	if i, ok := mapContentRouteEndpoint(leaves, []int{0, 1}, ""); !ok || i != 1 {
		t.Fatal("identical authoritative Place copies lost stable line representative")
	}
	if i, ok := mapContentRouteEndpoint(leaves, []int{2, 0, 1}, "place-a"); !ok || i != 1 {
		t.Fatal("explicit leg Place did not disambiguate other locations")
	}
	for _, c := range []struct {
		ids   []int
		place string
	}{{[]int{0, 2}, ""}, {[]int{0, 3}, ""}, {[]int{0, 4}, ""}, {[]int{0, 1}, "place-missing"}} {
		if _, ok := mapContentRouteEndpoint(leaves, c.ids, c.place); ok {
			t.Fatal("ambiguous or unloaded endpoint became a line")
		}
	}
	changed := append([]mapSpatialLeaf(nil), leaves...)
	changed[1].pin.Anchor.Latitude = 9
	if _, ok := mapContentRouteEndpoint(changed, []int{0, 1}, "place-a"); ok {
		t.Fatal("same Place ID with conflicting coordinates was guessed")
	}
	if len(leaves) != 5 || leaves[0].pin.MarkerID != "anchor:z" || leaves[1].pin.MarkerID != "anchor:a" {
		t.Fatal("line representative merged original markers")
	}
}
