package placematch

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
)

func TestPlaceMatchDeterministicReviewedSupply(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	capacity := 20
	makeSupply := func(id string, suitable bool) Supply {
		codes := []string{"football"}
		if suitable {
			codes = []string{"badminton"}
		}
		return Supply{Place: foundation.Place{ID: id, CityID: "aberdeen-gb", Name: "市中心球馆",
			Source: foundation.Source{Freshness: "current"}},
			Venue: &venue.Public{PlaceID: id, CityID: "aberdeen-gb", Facts: venue.Facts{
				Capacity: &capacity, Suitability: codes, ReservationSupport: "contact"}, ExpiresAt: now.Add(48 * time.Hour)}}
	}
	intent := socialintent.Record{ID: "intent", CreatorID: "owner", Type: "FIND_ACTIVITY", Status: socialintent.Active,
		Modality: "IN_PERSON", ExpiresAt: now.Add(24 * time.Hour),
		Constraints: json.RawMessage(`{"category":"badminton","areaLabel":"市中心","maxParticipants":10}`)}
	in := Inputs{PersonID: "owner", Intent: intent, CityID: "aberdeen-gb", Supply: []Supply{
		makeSupply("b", true), makeSupply("a", true), makeSupply("c", false),
		{Place: foundation.Place{ID: "d", CityID: "aberdeen-gb", Name: "市中心公园"}},
	}}
	got := Generate(now, in)
	if len(got) != 2 || got[0].Place.ID != "a" || got[1].Place.ID != "b" ||
		got[0].Action.Type != "OPEN_PLACE" || got[0].Reason == "" ||
		!got[0].HasReviewedVenue || got[0].Capacity == nil || *got[0].Capacity != 20 {
		t.Fatalf("reviewed Place matches: %+v", got)
	}
	if again := Generate(now, in); !reflect.DeepEqual(got, again) {
		t.Fatalf("nondeterministic: %+v %+v", got, again)
	}
	in.Intent.Type = "FIND_COMPANION"
	if companion := Generate(now, in); len(companion) != 2 {
		t.Fatalf("companion intent lost reviewed Place options: %+v", companion)
	}
	in.Intent.Type = "FIND_ACTIVITY"
	for _, tc := range []struct {
		name   string
		mutate func(*Inputs)
	}{
		{"different owner", func(i *Inputs) { i.Intent.CreatorID = "other" }},
		{"draft", func(i *Inputs) { i.Intent.Status = "DRAFT" }},
		{"online", func(i *Inputs) { i.Intent.Modality = "ONLINE" }},
		{"expired", func(i *Inputs) { i.Intent.ExpiresAt = now }},
		{"other city", func(i *Inputs) { i.CityID = "london" }},
		{"no venue", func(i *Inputs) { i.Supply = nil }},
		{"insufficient capacity", func(i *Inputs) {
			v := *i.Supply[0].Venue
			n := 5
			v.Capacity = &n
			i.Supply[0].Venue = &v
			i.Supply = i.Supply[:1]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := in
			copy.Supply = append([]Supply{}, in.Supply...)
			tc.mutate(&copy)
			if g := Generate(now, copy); len(g) != 0 {
				t.Fatalf("unsafe match: %+v", g)
			}
		})
	}
}

func TestPlaceMatchSemanticPublicRankingAndHardVenueBoundary(t *testing.T) {
	at := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	ids := []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"}
	capacity := 20
	in := Inputs{PersonID: "owner", CityID: "aberdeen-gb", Intent: socialintent.Record{ID: "intent", CreatorID: "owner", Type: "ORGANIZE", Status: socialintent.Active, Modality: "IN_PERSON", ExpiresAt: at.Add(time.Hour), Constraints: json.RawMessage(`{"category":"badminton","areaLabel":"市中心","minParticipants":8,"maxParticipants":10}`)}}
	for _, id := range ids {
		in.Supply = append(in.Supply, Supply{Place: foundation.Place{ID: id, CityID: in.CityID, Name: "市中心球馆", Source: foundation.Source{Freshness: "current"}}, Venue: &venue.Public{PlaceID: id, CityID: in.CityID, Facts: venue.Facts{Capacity: &capacity, Suitability: []string{"badminton"}}, ExpiresAt: at.Add(time.Hour)}})
	}
	p := pp.Public{SchemaVersion: pp.SchemaVersion, PlaceID: ids[1], CityID: in.CityID, Version: 1, Facts: pp.Facts{GoodFor: []string{"badminton"}, GroupSize: &pp.GroupSize{Min: 2, Max: 12}, Price: &pp.Price{Currency: "GBP", MinMinor: 0, MaxMinor: 1200, Unit: "per_person"}}, Confidence: pp.Assessment{Kind: "EDITOR_ASSESSMENT_UNCALIBRATED", Level: "MEDIUM"}, Source: pp.Source{Label: "本地合成审核来源", URL: "https://example.invalid/semantic", ObservedAt: at.Add(-time.Hour), ReviewedAt: at, ExpiresAt: at.Add(time.Hour)}}
	in.Supply[1].Profile = &p
	got := Generate(at, in)
	if len(got) != 2 || got[0].Place.ID != ids[1] || got[0].SemanticScore != 5 || got[1].SemanticScore != 0 || got[0].SemanticProfile == nil || !contains(got[0].Reason, "审核判断，非概率") {
		t.Fatal("semantic ranking", got)
	}
	for _, mode := range []string{"expired", "wrong_place", "wrong_city", "future_review", "invalid_confidence", "insufficient_capacity", "wrong_venue_category", "no_venue", "unknown_group"} {
		t.Run(mode, func(t *testing.T) {
			copy := in
			copy.Supply = append([]Supply{}, in.Supply...)
			q := p
			v := *in.Supply[1].Venue
			copy.Supply[1].Profile = &q
			copy.Supply[1].Venue = &v
			matchExpected := true
			switch mode {
			case "expired":
				q.Source.ExpiresAt = at
			case "wrong_place":
				q.PlaceID = ids[0]
			case "wrong_city":
				q.CityID = "another"
			case "future_review":
				q.Source.ReviewedAt = at.Add(time.Second)
			case "invalid_confidence":
				q.Confidence.Kind = "PROBABILITY"
			case "insufficient_capacity":
				n := 5
				v.Capacity = &n
				matchExpected = false
			case "wrong_venue_category":
				v.Suitability = []string{"football"}
				matchExpected = false
			case "no_venue":
				copy.Supply[1].Venue = nil
				matchExpected = false
			case "unknown_group":
				q.Facts.GroupSize = nil
			}
			g := Generate(at, copy)
			if !matchExpected {
				if len(g) != 1 || g[0].Place.ID != ids[0] {
					t.Fatal("soft fact bypassed hard Venue", g)
				}
				return
			}
			if mode == "unknown_group" {
				if g[0].SemanticScore != 2 {
					t.Fatal("invented group", g)
				}
				return
			}
			if len(g) != 2 || g[0].Place.ID != ids[0] || g[1].SemanticProfile != nil || g[1].SemanticScore != 0 {
				t.Fatal("invalid source influenced ranking", g)
			}
		})
	}
	// No category/group preference means no price, vibe or accessibility guess.
	in.Intent.Constraints = json.RawMessage(`{"areaLabel":"市中心"}`)
	if g := Generate(at, in); len(g) != 2 || g[0].Place.ID != ids[0] || g[1].SemanticScore != 0 {
		t.Fatal("guessed undeclared preference", g)
	}
}

func TestExplicitPlaceWithoutVenueStatesUnknown(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	const placeID = "22222222-2222-4222-8222-222222222222"
	in := Inputs{PersonID: "owner", Intent: socialintent.Record{ID: "intent", CreatorID: "owner",
		Type: "ORGANIZE", Status: socialintent.Active, Modality: "IN_PERSON",
		ExpiresAt: now.Add(time.Hour), Constraints: json.RawMessage(`{"placeId":"` + placeID + `"}`)},
		Supply: []Supply{{Place: foundation.Place{ID: placeID, CityID: "aberdeen-gb", Name: "公园"}}}}
	got := Generate(now, in)
	if len(got) != 1 || got[0].HasReviewedVenue || got[0].ReasonCodes[len(got[0].ReasonCodes)-1] != "VENUE_UNKNOWN" {
		t.Fatalf("explicit Place uncertainty: %+v", got)
	}
	in.Intent.Constraints = json.RawMessage(`{"placeId":"` + placeID + `","minParticipants":8}`)
	if got = Generate(now, in); len(got) != 0 {
		t.Fatalf("unknown capacity claimed sufficient: %+v", got)
	}
}
