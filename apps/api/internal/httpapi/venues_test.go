package httpapi

import (
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/venue"
)

func TestVenueCandidateValidation(t *testing.T) {
	base := venue.SubmitInput{
		Facts:     venue.Facts{ReservationSupport: "contact", Suitability: []string{"badminton"}},
		SourceURL: "https://example.org/venue", RightsNote: "Synthetic source terms",
		ExpiresAt: time.Now().Add(48 * time.Hour),
	}
	if !normalizeVenueInput(&base) {
		t.Fatal("valid sourced facts rejected")
	}
	bad := []struct {
		name   string
		change func(*venue.SubmitInput)
	}{
		{"no known fact", func(v *venue.SubmitInput) { v.ReservationSupport = "unknown"; v.Suitability = nil }},
		{"insecure source", func(v *venue.SubmitInput) { v.SourceURL = "http://example.org/venue" }},
		{"invalid capacity", func(v *venue.SubmitInput) { n := 0; v.Capacity = &n }},
		{"unsafe booking link", func(v *venue.SubmitInput) {
			s := "http://example.org/book"
			v.ReservationSupport = "external_url"
			v.ReservationURL = &s
		}},
		{"duplicate suitability", func(v *venue.SubmitInput) { v.Suitability = []string{"badminton", "badminton"} }},
		{"invalid operator", func(v *venue.SubmitInput) { s := "unknown"; v.OperatorOrganizationID = &s }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			tc.change(&v)
			if normalizeVenueInput(&v) {
				t.Fatal("invalid Venue facts accepted")
			}
		})
	}
}
