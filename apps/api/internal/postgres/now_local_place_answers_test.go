package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// These synthetic facts exercise pure functions only. They are neither intake
// candidates nor verified source data and are never written to a database.
func nowLocalPlaceTestFacts() (foundation.Place, []cityseed.PlaceFieldEvidence) {
	lat, lng := 57.15, -2.1
	p := foundation.Place{ID: "synthetic-place", Name: "Synthetic venue", AddressLabel: "Synthetic street", Location: foundation.Location{CoordinateSystem: "wgs84", Precision: "point", Latitude: &lat, Longitude: &lng}}
	fields := []cityseed.PlaceFieldEvidence{
		{PlaceID: p.ID, FieldKey: "name", Value: json.RawMessage(`"Synthetic venue"`)},
		{PlaceID: p.ID, FieldKey: "address", Value: json.RawMessage(`"Synthetic street"`)},
		{PlaceID: p.ID, FieldKey: "coordinates", Value: json.RawMessage(`{"coordinateSystem":"wgs84","latitude":57.15,"longitude":-2.1}`)},
	}
	return p, fields
}

func TestNowLocalPlaceFactsMatchCanonicalEntityExactly(t *testing.T) {
	p, fields := nowLocalPlaceTestFacts()
	if by, ok := localFieldsForPlace(p, fields); !ok || len(by) != 3 {
		t.Fatal("matching canonical entity rejected")
	}
	for _, tt := range []struct {
		name string
		key  string
		raw  string
	}{
		{"different name", "name", `"Another venue"`},
		{"different address", "address", `"Another street"`},
		{"different latitude", "coordinates", `{"coordinateSystem":"wgs84","latitude":57.16,"longitude":-2.1}`},
		{"different longitude", "coordinates", `{"coordinateSystem":"wgs84","latitude":57.15,"longitude":-2.11}`},
		{"different CRS", "coordinates", `{"coordinateSystem":"gcj02","latitude":57.15,"longitude":-2.1}`},
		{"malformed coordinate", "coordinates", `"unknown"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := append([]cityseed.PlaceFieldEvidence(nil), fields...)
			for i := range changed {
				if changed[i].FieldKey == tt.key {
					changed[i].Value = json.RawMessage(tt.raw)
				}
			}
			if _, ok := localFieldsForPlace(p, changed); ok {
				t.Fatal("different/malformed source fact accepted for canonical card entity")
			}
		})
	}
	for i := range fields {
		if _, ok := localFieldsForPlace(p, append(append([]cityseed.PlaceFieldEvidence(nil), fields[:i]...), fields[i+1:]...)); ok {
			t.Fatalf("missing %s accepted as sufficient", fields[i].FieldKey)
		}
	}
	p.Location.Latitude = nil
	if _, ok := localFieldsForPlace(p, fields); ok {
		t.Fatal("point without native coordinates accepted")
	}
}

func TestNowLocalPlaceUnknownAndConflictingFoodFacts(t *testing.T) {
	p, fields := nowLocalPlaceTestFacts()
	a := cityseed.PlaceFieldEvidence{FieldKey: "food_spicy", Value: json.RawMessage(`true`)}
	b := cityseed.PlaceFieldEvidence{FieldKey: "food_spicy", Value: json.RawMessage(`false`)}
	for _, pair := range [][]cityseed.PlaceFieldEvidence{{a, b}, {b, a}} {
		if _, ok := localFieldsForPlace(p, append(append([]cityseed.PlaceFieldEvidence(nil), fields...), pair...)); ok {
			t.Fatal("conflicting food source selected")
		}
	}
	if _, ok := localFieldsForPlace(p, append(append([]cityseed.PlaceFieldEvidence(nil), fields...), a, a)); !ok {
		t.Fatal("agreeing evidence rejected")
	}
	for _, raw := range []string{"", `"yes"`, `0`, `{}`, `null`} {
		t.Run("unknown:"+raw, func(t *testing.T) {
			facts := localFoodFacts(map[string]cityseed.PlaceFieldEvidence{"food_spicy": {Value: json.RawMessage(raw)}})
			if facts.Seafood != "UNKNOWN" || facts.Spicy != "UNKNOWN" {
				t.Fatalf("unknown/non-boolean food facts became a claim: %+v", facts)
			}
		})
	}
	facts := localFoodFacts(map[string]cityseed.PlaceFieldEvidence{"food_seafood": {Value: json.RawMessage(`true`)}, "food_spicy": {Value: json.RawMessage(`false`)}})
	if facts.Seafood != "yes" || facts.Spicy != "no" {
		t.Fatalf("explicit boolean facts lost: %+v", facts)
	}
}

func TestNowLocalPlaceCitationsUseEachEntityActualSources(t *testing.T) {
	field := func(key, sourceURL string) cityseed.PlaceFieldEvidence {
		return cityseed.PlaceFieldEvidence{FieldKey: key, Permission: cityseed.SourcePermission{SourceURL: sourceURL}}
	}
	sources := map[string]string{"https://example.invalid/a": "source-2", "https://example.invalid/b": "source-3", "https://example.invalid/c": "source-7"}
	first := []cityseed.PlaceFieldEvidence{field("name", "https://example.invalid/a"), field("address", "https://example.invalid/b"), field("coordinates", "https://example.invalid/a"), field("food_spicy", "https://example.invalid/c")}
	second := []cityseed.PlaceFieldEvidence{field("name", "https://example.invalid/c"), field("address", "https://example.invalid/c"), field("name", "https://example.invalid/unmapped")}
	if got := localPlaceCitations(first, sources, false); got != " [source-2] [source-3]" {
		t.Fatalf("address facts attributed to wrong/shared fixed source: %q", got)
	}
	if got := localPlaceCitations(first, sources, true); got != " [source-2] [source-3] [source-7]" {
		t.Fatalf("recommendation food source missing or duplicate: %q", got)
	}
	if got := localPlaceCitations(second, sources, false); got != " [source-7]" {
		t.Fatalf("second entity inherited first entity's citation: %q", got)
	}
	if got := localPlaceCitations([]cityseed.PlaceFieldEvidence{field("name", "https://example.invalid/unmapped")}, sources, false); got != "" {
		t.Fatalf("invented citation for unmapped evidence: %q", got)
	}
}

func TestNowLocalPlaceEntryRejectsMissingAuthorityBeforeSQL(t *testing.T) {
	validShape := arp.Access{Actor: identity.Actor{ID: "synthetic-person", AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: "synthetic-task", ExpectedTask: json.RawMessage(`{}`)}
	for _, tt := range []struct {
		name   string
		change func(*arp.Access)
	}{
		{"organization", func(a *arp.Access) { a.Actor.AccountType = "organization" }},
		{"missing actor", func(a *arp.Access) { a.Actor.ID = "" }},
		{"missing session", func(a *arp.Access) { a.SessionDigest = [32]byte{} }},
		{"missing task", func(a *arp.Access) { a.TaskID = "" }},
		{"missing expected version", func(a *arp.Access) { a.ExpectedTask = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := validShape
			tt.change(&a)
			for _, store := range []*Store{nil, {}} {
				reply, handled, err := store.TryOwnLocalPlaceReply(context.Background(), a)
				if reply != nil || handled || !errors.Is(err, arp.ErrDenied) {
					t.Fatalf("missing authority reached SQL or produced reply: reply=%v handled=%v err=%v", reply, handled, err)
				}
			}
		})
	}
}
