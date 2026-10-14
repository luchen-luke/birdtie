package saved

import (
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"strings"
	"testing"
	"time"
)

func TestPlacePhotoSavedOnlyCurrentAvailableOriginalPlace(t *testing.T) {
	now := time.Now().UTC()
	id := "9931ef56-8c9d-4241-9437-99373492c5a1"
	m := &pp.Metadata{EntityType: "place", EntityID: id, Version: 1, ThumbnailPath: pp.Path(id, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(30 * time.Second)}
	i := Item{Kind: "place", TargetID: id, Available: true, PlacePhoto: m}
	if !i.ValidPlacePhoto(now) {
		t.Fatal("valid place")
	}
	i.Available = false
	if i.ValidPlacePhoto(now) {
		t.Fatal("unavailable source granted photo")
	}
	i.Available = true
	i.Kind = "activity"
	if i.ValidPlacePhoto(now) {
		t.Fatal("activity borrowed place photo")
	}
	i.Kind = "place"
	m.ExpiresAt = now
	if i.ValidPlacePhoto(now) {
		t.Fatal("expired media revived")
	}
}
