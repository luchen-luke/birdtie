package agentresultprojection

import (
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"strings"
	"testing"
	"time"
)

func TestPlacePhotoTypedProjectionCannotBorrowAnotherSubject(t *testing.T) {
	id := "9931ef56-8c9d-4241-9437-99373492c5a1"
	m := &pp.Metadata{EntityType: "place", EntityID: id, Version: 1, ThumbnailPath: pp.Path(id, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: time.Now().UTC().Add(30 * time.Second)}
	i := Item{Entity: Ref{Type: "place", ID: id}, Title: "获准地点", Scope: AuthorizedView, PlacePhoto: m}
	if ValidateItem(i) != nil {
		t.Fatal("same original place rejected")
	}
	for _, kind := range []string{"person", "activity", "organization", "opportunity", "community"} {
		bad := i
		bad.Entity.Type = kind
		if ValidateItem(bad) == nil {
			t.Fatal("place photo borrowed", kind)
		}
	}
	bad := i
	bad.Entity.ID = "9931ef56-8c9d-4241-9437-99373492c5a2"
	if ValidateItem(bad) == nil {
		t.Fatal("another place borrowed photo")
	}
}
