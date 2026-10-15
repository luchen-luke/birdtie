package legacyplacereference

import (
	"strings"
	"testing"
	"time"
)

func TestOwnedReferenceIsBoundedAndNeverInventsPoint(t *testing.T) {
	now := time.Now().UTC()
	ref := Reference{"saved_place", "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	v := Detail{SchemaVersion: SchemaVersion, Reference: Summary{Reference: ref, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), SourceVersion: strings.Repeat("a", 64)}, Name: "原收藏地点", LocationPrecision: "none"}
	if !v.Valid(ref, now) {
		t.Fatal("private label without a point")
	}
	lat, lon := 0.0, 0.0
	v.Latitude, v.Longitude = &lat, &lon
	if v.Valid(ref, now) {
		t.Fatal("invented CRS")
	}
	v.CoordinateSystem, v.LocationPrecision = "wgs84", "point"
	if !v.Valid(ref, now) {
		t.Fatal("real WGS84 zero coordinate is valid")
	}
	v.Reference.ValidUntil = now.Add(31 * time.Second)
	if v.Valid(ref, now) {
		t.Fatal("unbounded private lease")
	}
	v.Reference.ValidUntil = now
	if v.Valid(ref, now) {
		t.Fatal("expired private lease")
	}
	other := ref
	other.RecordID = ref.PlaceID
	v.Reference.ValidUntil = now.Add(time.Second)
	if v.Valid(other, now) {
		t.Fatal("reference substitution")
	}
}
