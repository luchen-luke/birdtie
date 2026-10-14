package saved

import (
	"math"
	"testing"
	"time"
)

func TestSavedPointPublicPairedCurrentBoundedOnly(t *testing.T) {
	now := time.Now().UTC()
	lat, lng := 57.15, -2.1
	end := now.Add(60 * time.Second)
	item := Item{Kind: "place", Available: true, Latitude: &lat, Longitude: &lng, SourceExpiresAt: &end}
	if !item.ValidPoint(now) {
		t.Fatal("current public point rejected")
	}
	for _, bad := range []Item{{Kind: "group", Available: true, Latitude: &lat, Longitude: &lng, SourceExpiresAt: &end}, {Kind: "place", Latitude: &lat, Longitude: &lng, SourceExpiresAt: &end}, {Kind: "place", Available: true, Latitude: &lat}, {Kind: "place", Available: true, SourceExpiresAt: &end}} {
		if bad.ValidPoint(now) {
			t.Fatal("private/type/unpaired point accepted")
		}
	}
	for _, value := range []float64{91, math.NaN(), math.Inf(1)} {
		item.Latitude = &value
		if item.ValidPoint(now) {
			t.Fatal("invalid coordinate accepted")
		}
	}
	item.Latitude = &lat
	old := now
	item.SourceExpiresAt = &old
	if item.ValidPoint(now) {
		t.Fatal("expired point accepted")
	}
	far := now.Add(time.Hour)
	item.SourceExpiresAt = &far
	if item.ValidPoint(now) {
		t.Fatal("invented lease accepted")
	}
}
