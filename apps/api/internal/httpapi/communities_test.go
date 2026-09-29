package httpapi

import (
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/community"
)

func TestValidCommunityInput(t *testing.T) {
	base := community.Input{
		Name: " Aberdeen players ", SourceLabel: "Club",
		SourceURL: "https://example.org/club", RightsNote: "Owner granted publishing rights.",
		ExpiresAt: time.Now().Add(90 * 24 * time.Hour),
	}
	if !validCommunityInput(&base) || base.Name != "Aberdeen players" {
		t.Fatal("valid owner submission was rejected or not normalized")
	}
	for _, change := range []func(*community.Input){
		func(i *community.Input) { i.SourceURL = "http://example.org/club" },
		func(i *community.Input) { i.RightsNote = "unknown" },
		func(i *community.Input) { i.ExpiresAt = time.Now().Add(-time.Hour) },
		func(i *community.Input) { i.PlaceID = "not-a-uuid" },
	} {
		invalid := base
		change(&invalid)
		if validCommunityInput(&invalid) {
			t.Fatalf("unsafe community input accepted: %+v", invalid)
		}
	}
}
