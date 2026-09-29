package httpapi

import (
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/intent"
)

func TestValidIntentInput(t *testing.T) {
	now := time.Now().UTC()
	input := intent.Input{
		Confirmed: true,
		Topic:     " badminton ", Details: " beginner-friendly ",
		AvailableFrom: now.Add(time.Minute), AvailableUntil: now.Add(7 * 24 * time.Hour),
		ExpiresAt: now.Add(7 * 24 * time.Hour), TimeZone: "Europe/London",
		CoarseAreaLabel: " Aberdeen centre ",
	}
	if !validIntentInput(&input) {
		t.Fatal("valid intent rejected")
	}
	if input.Topic != "badminton" || input.CoarseAreaLabel != "Aberdeen centre" {
		t.Fatal("intent text not normalized")
	}
	invalid := input
	invalid.Confirmed = false
	if validIntentInput(&invalid) {
		t.Fatal("unconfirmed intent accepted")
	}
	invalid = input
	invalid.TimeZone = "Not/AZone"
	if validIntentInput(&invalid) {
		t.Fatal("invalid time zone accepted")
	}
	invalid = input
	invalid.AvailableUntil = now.Add(40 * 24 * time.Hour)
	if validIntentInput(&invalid) {
		t.Fatal("long availability accepted")
	}
	invalid = input
	invalid.ExpiresAt = now.Add(-time.Hour)
	if validIntentInput(&invalid) {
		t.Fatal("expired intent accepted")
	}
}
