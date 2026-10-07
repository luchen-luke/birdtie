package httpapi

import (
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
)

func TestValidManagedActivity(t *testing.T) {
	start := time.Now().UTC().Add(48 * time.Hour)
	valid := activitypublish.Input{
		CityID: "aberdeen-gb", Title: "  周末羽毛球  ",
		StartsAt: start, EndsAt: start.Add(2 * time.Hour),
		TimeZone: "Europe/London", Visibility: "public",
	}
	if got := validManagedActivity(&valid); got != "" || valid.Title != "周末羽毛球" {
		t.Fatalf("valid input: code=%q title=%q", got, valid.Title)
	}
	tests := []struct {
		name string
		edit func(*activitypublish.Input)
		want string
	}{
		{"short title", func(i *activitypublish.Input) { i.Title = "A" }, "title_length_invalid"},
		{"reversed schedule", func(i *activitypublish.Input) { i.EndsAt = i.StartsAt }, "schedule_invalid"},
		{"long event", func(i *activitypublish.Input) { i.EndsAt = i.StartsAt.Add(8 * 24 * time.Hour) }, "schedule_too_long"},
		{"unknown zone", func(i *activitypublish.Input) { i.TimeZone = "Moon/Base" }, "timeZone_invalid"},
		{"negative price", func(i *activitypublish.Input) { i.PriceMinor = -1 }, "priceMinor_invalid"},
		{"currency required", func(i *activitypublish.Input) { i.PriceMinor = 100 }, "currency_required"},
		{"invalid capacity", func(i *activitypublish.Input) { n := 0; i.Capacity = &n }, "capacity_invalid"},
		{"invalid visibility", func(i *activitypublish.Input) { i.Visibility = "secret" }, "visibility_invalid"},
		{"in person TBD", func(i *activitypublish.Input) { i.Modality, i.PhysicalPlaceStatus = "in_person", "tbd" }, ""},
		{"online", func(i *activitypublish.Input) { i.Modality, i.PhysicalPlaceStatus = "online", "not_applicable" }, ""},
		{"hybrid TBD", func(i *activitypublish.Input) { i.Modality, i.PhysicalPlaceStatus = "hybrid", "tbd" }, ""},
		{"hybrid confirmed", func(i *activitypublish.Input) {
			i.Modality, i.PhysicalPlaceStatus, i.PlaceID = "hybrid", "confirmed", "b1700000-0000-4000-8000-000000000004"
		}, ""},
		{"hybrid confirmed without place", func(i *activitypublish.Input) {
			i.Modality, i.PhysicalPlaceStatus = "hybrid", "confirmed"
		}, "activity_location_invalid"},
		{"online with place", func(i *activitypublish.Input) {
			i.Modality, i.PhysicalPlaceStatus, i.PlaceID = "online", "not_applicable", "b1700000-0000-4000-8000-000000000004"
		}, "activity_location_invalid"},
		{"in person without place or TBD", func(i *activitypublish.Input) { i.Modality, i.PhysicalPlaceStatus = "in_person", "confirmed" }, "activity_location_invalid"},
		{"venue differs from place", func(i *activitypublish.Input) {
			i.Modality, i.PhysicalPlaceStatus, i.PlaceID, i.VenuePlaceID = "in_person", "confirmed", "b1700000-0000-4000-8000-000000000004", "b1700000-0000-4000-8000-000000000007"
		}, "activity_location_invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.edit(&input)
			if got := validManagedActivity(&input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
