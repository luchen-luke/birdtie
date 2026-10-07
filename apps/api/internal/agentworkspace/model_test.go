package agentworkspace

import (
	"reflect"
	"testing"
)

func TestTermsFiltersCommonWordsAndDuplicates(t *testing.T) {
	got := Terms("Find someone to play Badminton, badminton this weekend near Aberdeen!")
	want := []string{"badminton", "aberdeen"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Terms() = %v, want %v", got, want)
	}
}

func TestTermsKeepsChineseTextAndLimitsTerms(t *testing.T) {
	got := Terms("羽毛球 周末 朋友 a b c d e f g")
	want := []string{"羽毛球", "周末", "朋友"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Terms() = %v, want %v", got, want)
	}
	if len(Terms("alpha beta gamma delta epsilon zeta eta theta")) != 6 {
		t.Fatal("Terms() must cap query terms")
	}
}

func TestMapBoundsValidateAndRoundTripThroughTaskFilters(t *testing.T) {
	bounds := MapBounds{West: -2.2, South: 57.1, East: -2.0, North: 57.3}
	if !bounds.Valid() {
		t.Fatal("valid city bounds rejected")
	}
	got, err := BoundsFromFilters(bounds.Filters())
	if err != nil || *got != bounds {
		t.Fatalf("bounds round trip = %#v, %v; want %#v", got, err, bounds)
	}
	invalid := []MapBounds{
		{West: 181, South: 0, East: 182, North: 1},
		{West: 1, South: 0, East: 0, North: 1},
		{West: 0, South: 91, East: 1, North: 92},
	}
	for _, bounds := range invalid {
		if bounds.Valid() {
			t.Fatalf("invalid map bounds accepted: %#v", bounds)
		}
	}
	if _, err := BoundsFromFilters(map[string]string{"mapWest": "bad"}); err == nil {
		t.Fatal("malformed persisted bounds accepted")
	}
}
