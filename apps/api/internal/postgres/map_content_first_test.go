package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
	"time"
)

func TestContentFirstStableChronology(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	a := content.MapContentPin{MomentID: "b", MarkerID: "anchor:b", ContentTime: &newer}
	b := content.MapContentPin{MomentID: "a", MarkerID: "anchor:a", ContentTime: &old}
	if !mapContentPinNewer(a, b) || mapContentPinNewer(b, a) {
		t.Fatal("time must precede ID")
	}
	b.ContentTime = &newer
	if !mapContentPinNewer(b, a) {
		t.Fatal("stable moment tie")
	}
	b.ContentTime = nil
	if !mapContentPinNewer(a, b) || mapContentPinNewer(b, a) {
		t.Fatal("unknown import timestamp must sort last")
	}
}
