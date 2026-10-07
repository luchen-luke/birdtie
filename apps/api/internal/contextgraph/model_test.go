package contextgraph

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestContextTypesWithoutCityParent(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, kind := range []Type{City, Country, Institution, Community, Online} {
		ref, err := Parse(string(kind), id)
		if err != nil || ref.Type != kind || ref.ID != id {
			t.Fatalf("%s: ref=%+v err=%v", kind, ref, err)
		}
		encoded, err := json.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Ref
		if err := json.Unmarshal(encoded, &roundTrip); err != nil || !ref.Equal(roundTrip) {
			t.Fatalf("%s JSON contract: %s %v", kind, encoded, err)
		}
	}
	city, _ := Parse("CITY", id)
	online, _ := Parse("ONLINE", id)
	if city.Equal(online) {
		t.Fatal("same UUID crossed context type boundary")
	}
	for _, tc := range []struct {
		kind, id string
		want     error
	}{{"PERSON", id, ErrType}, {"", id, ErrType}, {"ONLINE", "city-name", ErrID}} {
		if _, err := Parse(tc.kind, tc.id); !errors.Is(err, tc.want) {
			t.Fatalf("Parse(%q,%q) = %v, want %v", tc.kind, tc.id, err, tc.want)
		}
	}
}
