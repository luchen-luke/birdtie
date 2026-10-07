package opportunity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

func TestGenerateDeterministicVerifiedActivityCandidates(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	contextID := "11111111-1111-4111-8111-111111111111"
	placeID := "22222222-2222-4222-8222-222222222222"
	activityID := "33333333-3333-4333-8333-333333333333"
	intent := socialintent.Record{
		ID: "44444444-4444-4444-8444-444444444444", CreatorID: "person-a",
		Type: "FIND_ACTIVITY", Status: socialintent.Active, Modality: "IN_PERSON",
		ContextID: &contextID, ExpiresAt: now.Add(48 * time.Hour),
		Constraints: json.RawMessage(`{"category":"badminton","placeId":"` + placeID + `","areaLabel":"市中心"}`),
	}
	activity := foundation.Activity{
		ID: activityID, CityID: "aberdeen-gb", PlaceID: placeID,
		CategoryCode: "badminton", StartsAt: now.Add(24 * time.Hour),
		EndsAt: now.Add(26 * time.Hour), Status: "upcoming",
		Organizer: foundation.ActivityOrganizer{Type: "PERSON", ID: "friend-b"},
		Source:    foundation.Source{Freshness: "current"},
	}
	place := foundation.Place{
		ID: placeID, CityID: "aberdeen-gb", Name: "市中心体育馆",
		Source: foundation.Source{Freshness: "current"},
	}
	in := Inputs{
		PersonID: "person-a", Intents: []socialintent.Record{intent},
		Supply:         []Supply{{Activity: activity, Place: place}},
		ContextCities:  map[string]string{contextID: "aberdeen-gb"},
		DeclaredCities: map[string]string{"aberdeen-gb": "current"},
		TiedPeople:     map[string]bool{"friend-b": true},
	}
	got := Generate(now, in)
	if len(got) != 1 {
		t.Fatalf("candidates: %+v", got)
	}
	item := got[0]
	if item.ID != intent.ID+":"+activityID || item.Entity != (EntityRef{Type: "ACTIVITY", ID: activityID}) ||
		item.Place != (EntityRef{Type: "PLACE", ID: placeID}) || item.Action.Target != item.Entity ||
		item.Action.Type != "OPEN_ACTIVITY" || item.RuleVersion != RuleVersion || item.Reason == "" {
		t.Fatalf("typed candidate: %+v", item)
	}
	wantReasons := []string{"INTENT_CATEGORY", "INTENT_PLACE", "INTENT_CITY_CONTEXT", "TIE_ORGANIZER"}
	if !reflect.DeepEqual(item.ReasonCodes, wantReasons) {
		t.Fatalf("reasons: %v", item.ReasonCodes)
	}
	if other := Generate(now, in); !reflect.DeepEqual(got, other) {
		t.Fatalf("nondeterministic output: %+v vs %+v", got, other)
	}
	for _, tc := range []struct {
		name   string
		change func(*Inputs)
	}{
		{"no supply", func(i *Inputs) { i.Supply = nil }},
		{"other owner", func(i *Inputs) { i.Intents[0].CreatorID = "person-b" }},
		{"draft", func(i *Inputs) { i.Intents[0].Status = "DRAFT" }},
		{"expired intent", func(i *Inputs) { i.Intents[0].ExpiresAt = now }},
		{"online has no verified Activity supply", func(i *Inputs) { i.Intents[0].Modality = "ONLINE" }},
		{"wrong city context", func(i *Inputs) { i.ContextCities[contextID] = "other-city" }},
		{"missing context", func(i *Inputs) { i.ContextCities = nil }},
		{"wrong place", func(i *Inputs) { i.Supply[0].Place.ID = "other-place" }},
		{"wrong category", func(i *Inputs) { i.Supply[0].Activity.CategoryCode = "basketball" }},
		{"ended", func(i *Inputs) { i.Supply[0].Activity.EndsAt = now }},
		{"expired place", func(i *Inputs) { i.Supply[0].Place.Source.Freshness = "expired" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := Inputs{
				PersonID: in.PersonID, Intents: append([]socialintent.Record{}, in.Intents...),
				Supply:         append([]Supply{}, in.Supply...),
				ContextCities:  map[string]string{contextID: "aberdeen-gb"},
				DeclaredCities: in.DeclaredCities, TiedPeople: in.TiedPeople,
			}
			tc.change(&copy)
			if got := Generate(now, copy); len(got) != 0 {
				t.Fatalf("unsafe candidate: %+v", got)
			}
		})
	}
}

func TestGenerateDoesNotInferAreaOrAnonymousGlobalIntent(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	base := Inputs{PersonID: "owner", Intents: []socialintent.Record{{
		ID: "intent", CreatorID: "owner", Type: "FIND_ACTIVITY", Status: socialintent.Active,
		Modality: "IN_PERSON", ExpiresAt: now.Add(time.Hour),
		Constraints: json.RawMessage(`{"areaLabel":"市中心"}`),
	}}, Supply: []Supply{{
		Activity: foundation.Activity{ID: "activity", CityID: "aberdeen", PlaceID: "place", EndsAt: now.Add(time.Hour), Status: "upcoming"},
		Place:    foundation.Place{ID: "place", CityID: "aberdeen", Name: "远郊体育场"},
	}}}
	if got := Generate(now, base); len(got) != 0 {
		t.Fatalf("inferred area match: %+v", got)
	}
	base.Intents[0].Constraints = json.RawMessage(`{}`)
	if got := Generate(now, base); len(got) != 0 {
		t.Fatalf("global unconstrained suggestion: %+v", got)
	}
}

func TestGenerateRanksExistingTieThenJoinedCommunityThenOtherSupply(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	intent := socialintent.Record{
		ID: "intent", CreatorID: "owner", Type: "FIND_ACTIVITY", Status: socialintent.Active,
		Audience: "FRIENDS", Modality: "IN_PERSON", ExpiresAt: now.Add(48 * time.Hour),
		Constraints: json.RawMessage(`{"category":"badminton","areaLabel":"城区"}`),
	}
	makeSupply := func(id, kind, organizer, visibility string, hours int) Supply {
		return Supply{
			Activity: foundation.Activity{
				ID: id, CityID: "aberdeen", PlaceID: "place-" + id,
				CategoryCode: "badminton", StartsAt: now.Add(time.Duration(hours) * time.Hour),
				EndsAt: now.Add(time.Duration(hours+2) * time.Hour), Status: "upcoming",
				Visibility: visibility, Organizer: foundation.ActivityOrganizer{Type: kind, ID: organizer},
			},
			Place: foundation.Place{ID: "place-" + id, CityID: "aberdeen", Name: "城区体育馆"},
		}
	}
	in := Inputs{
		PersonID: "owner", Intents: []socialintent.Record{intent},
		Supply: []Supply{
			makeSupply("public", "PERSON", "stranger", "public", 1),
			makeSupply("other", "PERSON", "stranger-2", "invite_only", 2),
			makeSupply("followed", "ORGANIZATION", "followed-org", "public", 2),
			makeSupply("followed-private", "ORGANIZATION", "followed-org", "invite_only", 2),
			makeSupply("community", "COMMUNITY", "joined-community", "public", 3),
			makeSupply("friend", "PERSON", "friend", "public", 4),
		},
		TiedPeople:         map[string]bool{"friend": true},
		JoinedCommunities:  map[string]bool{"joined-community": true},
		FollowedOrganizers: map[string]bool{"ORGANIZATION:followed-org": true},
	}
	got := Generate(now, in)
	want := []string{TierExistingTie, TierSharedCommunity, TierFollowedPublic,
		TierPublic, TierOtherVisible, TierOtherVisible}
	if len(got) != len(want) {
		t.Fatalf("candidates: %+v", got)
	}
	for i, tier := range want {
		if got[i].RouteTier != tier {
			t.Fatalf("tier %d: %s want %s", i, got[i].RouteTier, tier)
		}
	}
	if got[0].Entity.ID != "friend" || got[1].Entity.ID != "community" ||
		got[2].Entity.ID != "followed" ||
		!containsReason(got[0].ReasonCodes, "TIE_ORGANIZER") ||
		!containsReason(got[1].ReasonCodes, "JOINED_COMMUNITY") ||
		!containsReason(got[2].ReasonCodes, "FOLLOWED_ORGANIZER") ||
		containsReason(got[4].ReasonCodes, "FOLLOWED_ORGANIZER") {
		t.Fatalf("relationship ranking or reason: %+v", got)
	}
	in.TiedPeople = nil
	in.JoinedCommunities = nil
	in.FollowedOrganizers = nil
	withoutRelationships := Generate(now, in)
	if withoutRelationships[0].Entity.ID != "public" ||
		containsReason(withoutRelationships[0].ReasonCodes, "TIE_ORGANIZER") {
		t.Fatalf("revoked relationship still influences ranking: %+v", withoutRelationships)
	}
}

func containsReason(reasons []string, wanted string) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}

func TestOpportunityNamesAndReasonsUseOnlyAuthorizedSupply(t *testing.T) {
	now := time.Now()
	const placeID = "22222222-2222-4222-8222-222222222222"
	in := Inputs{PersonID: "owner", Intents: []socialintent.Record{{
		ID: "source", CreatorID: "owner", Type: "FIND_ACTIVITY", Status: socialintent.Active,
		Audience: "PRIVATE", Title: "PRIVATE_INTENT_TITLE_SENTINEL", Modality: "IN_PERSON",
		ExpiresAt: now.Add(time.Hour), Constraints: json.RawMessage(`{"category":"badminton","placeId":"` + placeID + `"}`),
	}}, Supply: []Supply{{
		Activity: foundation.Activity{ID: "activity", CityID: "city", PlaceID: placeID,
			Title: "当前有权查看的活动", HostLabel: "RAW_HOST_SENTINEL", Summary: "RAW_SUMMARY_SENTINEL",
			Description: "RAW_DESCRIPTION_SENTINEL", CategoryCode: "badminton", EndsAt: now.Add(time.Hour),
			Organizer: foundation.ActivityOrganizer{Type: "ORGANIZATION", ID: "organization", Name: "RAW_ORGANIZATION_SENTINEL"}},
		Place: foundation.Place{ID: placeID, CityID: "city", Name: "当前已发布地点", AddressLabel: "RAW_ADDRESS_SENTINEL"},
	}}}
	got := Generate(now, in)
	if len(got) != 1 || got[0].Title != in.Supply[0].Activity.Title || got[0].PlaceName != in.Supply[0].Place.Name {
		t.Fatalf("names did not come from authorized supply: %+v", got)
	}
	if !reflect.DeepEqual(got[0].ReasonCodes, []string{"INTENT_CATEGORY", "INTENT_PLACE", "ORGANIZATION_ACTIVITY"}) ||
		!strings.Contains(got[0].Reason, "这是一条组织主办的活动") || got[0].RuleVersion != "activity-place-v2" {
		t.Fatalf("typed organization explanation or compatibility: %+v", got[0])
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"PRIVATE_INTENT_TITLE_SENTINEL", "RAW_HOST_SENTINEL", "RAW_SUMMARY_SENTINEL", "RAW_DESCRIPTION_SENTINEL", "RAW_ORGANIZATION_SENTINEL", "RAW_ADDRESS_SENTINEL", "latitude", "longitude", "朋友也喜欢", "已核验"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("unapproved explanation field %s: %s", value, encoded)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "intentId", "entity", "place", "reasonCodes", "reason", "action", "ruleVersion", "routeTier", "startsAt", "title", "placeName"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing additive/legacy field %s", key)
		}
	}

	for _, tc := range []struct{ name, kind, id string }{
		{"person named as organization", "PERSON", "person"},
		{"community", "COMMUNITY", "community"},
		{"business", "BUSINESS", "business"},
		{"unknown organizer", "UNKNOWN", "unknown"},
		{"organization without typed id", "ORGANIZATION", ""},
		{"organization with blank typed id", "ORGANIZATION", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := in
			copy.Supply = append([]Supply{}, in.Supply...)
			copy.Supply[0].Activity.Organizer = foundation.ActivityOrganizer{Type: tc.kind, ID: tc.id, Name: "CSSA Organization"}
			copy.Supply[0].Activity.HostLabel = "CSSA Organization"
			copy.Supply[0].Activity.OrganizationID = new(string)
			*copy.Supply[0].Activity.OrganizationID = "legacy-organization-field"
			result := Generate(now, copy)
			if len(result) != 1 || containsReason(result[0].ReasonCodes, "ORGANIZATION_ACTIVITY") {
				t.Fatalf("inferred organization from legacy/name: %+v", result)
			}
		})
	}
}
