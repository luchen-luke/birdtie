package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

func TestPlaceSourcePermissionNativeRejectsBeforeAnySQL(t *testing.T) {
	for _, store := range []*Store{nil, {}} {
		if _, e := store.Submit(context.Background(), "actor", "aberdeen-gb", cityseed.SubmitInput{Name: "RESTRICTED_CONTENT_CANARY", SourceURL: "https://example.invalid/place", RightsNote: "Rights note alone is never a permission"}); !errors.Is(e, cityseed.ErrSourcePermission) {
			t.Fatalf("missing rights reached SQL: %v", e)
		}
	}
}
func TestPlaceSourcePermissionConflictingFactsDoNotChooseNewest(t *testing.T) {
	a := cityseed.PlaceFieldEvidence{FieldKey: "food_spicy", Value: json.RawMessage(`true`)}
	b := cityseed.PlaceFieldEvidence{FieldKey: "food_spicy", Value: json.RawMessage(`false`)}
	name := cityseed.PlaceFieldEvidence{FieldKey: "name", Value: json.RawMessage(`"Synthetic name"`)}
	for _, fields := range [][]cityseed.PlaceFieldEvidence{{a, b, name}, {b, a, name}} {
		out := removeConflictingPlaceFields(fields)
		if len(out) != 1 || out[0].FieldKey != "name" {
			t.Fatalf("contradictory fact selected %+v", out)
		}
	}
	if out := removeConflictingPlaceFields([]cityseed.PlaceFieldEvidence{a, a, name}); len(out) != 3 {
		t.Fatal("agreeing independent evidence lost")
	}
}
