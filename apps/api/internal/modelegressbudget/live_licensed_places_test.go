package modelegressbudget

import (
	"strings"
	"testing"
)

func licensedContextFixture() LiveLicensedPlaceContext {
	return LiveLicensedPlaceContext{Schema: LiveLicensedPlaceContextSchema, City: LiveSelectedCity{ID: "aberdeen", Name: "Aberdeen", CountryCode: "GB"}, Slots: LiveResolvedSlots{Operation: "FIND_PLACE", Target: "FIND_PLACE"}, Places: []LiveLicensedPlace{{Name: "Glenhouse", Address: "1 Test Road", SourceNumbers: []int{1}}}, Sources: []LivePublicSource{{Title: "OpenStreetMap contributors", URL: "https://www.openstreetmap.org/node/123", Passage: ""}}}
}

func TestLicensedPlacePayloadClosedBoundary(t *testing.T) {
	cases := map[string]func(*LiveLicensedPlaceContext){
		"no_places":          func(c *LiveLicensedPlaceContext) { c.Places = nil },
		"no_sources":         func(c *LiveLicensedPlaceContext) { c.Sources = nil },
		"invented_source":    func(c *LiveLicensedPlaceContext) { c.Places[0].SourceNumbers = []int{2} },
		"nonplace_target":    func(c *LiveLicensedPlaceContext) { c.Slots.Target = "FIND_ACTIVITY" },
		"unknown_category":   func(c *LiveLicensedPlaceContext) { c.Slots.Category = "PRIVATE_PROFILE" },
		"reserved_source":    func(c *LiveLicensedPlaceContext) { c.Sources[0].URL = "https://localhost/source" },
		"raw_source_passage": func(c *LiveLicensedPlaceContext) { c.Sources[0].Passage = "search provider raw response" },
		"too_many_places":    func(c *LiveLicensedPlaceContext) { c.Places = make([]LiveLicensedPlace, 11) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := licensedContextFixture()
			mutate(&c)
			if _, err := LiveLicensedPlaceModelPayload("解释这家地点", c); err == nil {
				t.Fatal("invalid context admitted")
			}
		})
	}
	if p, err := LiveLicensedPlaceModelPayload("解释这家地点", licensedContextFixture()); err != nil || !strings.Contains(p, "Glenhouse") {
		t.Fatalf("valid public fields rejected: %v", err)
	}
	if _, err := LiveLicensedPlaceModelPayload(strings.Repeat("x", 4096), licensedContextFixture()); err == nil {
		t.Fatal("oversized assembled message admitted")
	}
}
