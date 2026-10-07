package newpeople

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func onlineSignals() (Signal, Signal) {
	source := Signal{IntentID: "source-intent", AccountID: "source-person", Category: "羽毛球", Modality: "ONLINE"}
	peer := Signal{IntentID: "peer-intent", AccountID: "peer-person", DisplayName: "公开昵称", Category: "羽毛球", Modality: "ONLINE"}
	return source, peer
}

func TestMatchingExplicitConstraintMatrix(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Signal, *Signal)
		want   bool
		code   string
	}{
		{"online without platform is unconstrained", func(s, p *Signal) {}, true, ""},
		{"normalized category and modality", func(s, p *Signal) {
			s.Category, p.Category = " Board   Games ", "board games"
			s.Modality, p.Modality = " online ", "ONLINE"
		}, true, "CATEGORY_EQUAL"},
		{"blank category rejects", func(s, p *Signal) { s.Category, p.Category = " ", " " }, false, ""},
		{"different category rejects", func(s, p *Signal) { p.Category = "篮球" }, false, ""},
		{"different modality rejects", func(s, p *Signal) { p.Modality = "IN_PERSON" }, false, ""},
		{"unsupported modality rejects", func(s, p *Signal) { s.Modality, p.Modality = "UNKNOWN", "UNKNOWN" }, false, ""},
		{"one explicit source platform rejects unknown", func(s, p *Signal) { s.OnlinePlatform = "Zoom" }, false, ""},
		{"one explicit peer platform rejects unknown", func(s, p *Signal) { p.OnlinePlatform = "Zoom" }, false, ""},
		{"normalized shared platform", func(s, p *Signal) { s.OnlinePlatform, p.OnlinePlatform = " Video   Room ", "video room" }, true, "ONLINE_PLATFORM_EQUAL"},
		{"different platforms reject", func(s, p *Signal) { s.OnlinePlatform, p.OnlinePlatform = "Zoom", "Teams" }, false, ""},
		{"online does not require or compare city", func(s, p *Signal) { s.CityID, p.CityID = "city-a", "city-b" }, true, ""},
		{"online with physical place rejects", func(s, p *Signal) { s.PlaceID = "place-a" }, false, ""},
		{"online with physical area rejects", func(s, p *Signal) { p.AreaLabel = "centre" }, false, ""},
		{"same place needs no invented city", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.PlaceID, p.PlaceID = " place-a ", "PLACE-A"
		}, true, "PLACE_EQUAL"},
		{"same city and normalized area", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.CityID, p.CityID = "aberdeen-gb", "aberdeen-gb"
			s.AreaLabel, p.AreaLabel = " City   Centre ", "city centre"
		}, true, "CITY_AREA_EQUAL"},
		{"same global area does not establish scope", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.AreaLabel, p.AreaLabel = "city centre", "city centre"
		}, false, ""},
		{"different cities reject shared area text", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.CityID, p.CityID = "city-a", "city-b"
			s.AreaLabel, p.AreaLabel = "city centre", "city centre"
		}, false, ""},
		{"missing peer area rejects", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.CityID, p.CityID = "city-a", "city-a"
			s.AreaLabel = "city centre"
		}, false, ""},
		{"different explicit places cannot fall back to area", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.CityID, p.CityID = "city-a", "city-a"
			s.AreaLabel, p.AreaLabel = "city centre", "city centre"
			s.PlaceID, p.PlaceID = "place-a", "place-b"
		}, false, ""},
		{"one explicit place cannot fall back to area", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.CityID, p.CityID = "city-a", "city-a"
			s.AreaLabel, p.AreaLabel = "city centre", "city centre"
			s.PlaceID = "place-a"
		}, false, ""},
		{"in person cannot use online platform", func(s, p *Signal) {
			s.Modality, p.Modality = "IN_PERSON", "IN_PERSON"
			s.PlaceID, p.PlaceID = "place-a", "place-a"
			p.OnlinePlatform = "Zoom"
		}, false, ""},
		{"hybrid requires both physical and platform", func(s, p *Signal) {
			s.Modality, p.Modality = "HYBRID", "HYBRID"
			s.PlaceID, p.PlaceID = "place-a", "place-a"
			s.OnlinePlatform, p.OnlinePlatform = "Zoom", "zoom"
		}, true, "ONLINE_PLATFORM_EQUAL"},
		{"hybrid unknown platforms reject", func(s, p *Signal) {
			s.Modality, p.Modality = "HYBRID", "HYBRID"
			s.PlaceID, p.PlaceID = "place-a", "place-a"
		}, false, ""},
		{"hybrid differing physical scope rejects", func(s, p *Signal) {
			s.Modality, p.Modality = "HYBRID", "HYBRID"
			s.PlaceID, p.PlaceID = "place-a", "place-b"
			s.OnlinePlatform, p.OnlinePlatform = "Zoom", "zoom"
		}, false, ""},
		{"hybrid differing platform rejects", func(s, p *Signal) {
			s.Modality, p.Modality = "HYBRID", "HYBRID"
			s.PlaceID, p.PlaceID = "place-a", "place-a"
			s.OnlinePlatform, p.OnlinePlatform = "Zoom", "Teams"
		}, false, ""},
		{"same person rejects", func(s, p *Signal) { p.AccountID = s.AccountID }, false, ""},
		{"case normalized self rejects", func(s, p *Signal) { p.AccountID = " SOURCE-PERSON " }, false, ""},
		{"missing source id rejects", func(s, p *Signal) { s.IntentID = " " }, false, ""},
		{"missing candidate id rejects", func(s, p *Signal) { p.IntentID = "" }, false, ""},
		{"missing person id rejects", func(s, p *Signal) { p.AccountID = "" }, false, ""},
		{"unknown participant bounds do not fabricate requirement", func(s, p *Signal) {}, true, ""},
		{"overlapping explicit ranges", func(s, p *Signal) {
			s.MinParticipants, s.MaxParticipants, p.MinParticipants, p.MaxParticipants = 2, 4, 4, 6
		}, true, "PARTICIPANT_RANGE_COMPATIBLE"},
		{"disjoint ranges reject", func(s, p *Signal) {
			s.MinParticipants, s.MaxParticipants, p.MinParticipants, p.MaxParticipants = 2, 3, 4, 6
		}, false, ""},
		{"unknown upper bound remains unconstrained", func(s, p *Signal) { s.MinParticipants, p.MinParticipants, p.MaxParticipants = 4, 4, 6 }, true, "PARTICIPANT_RANGE_COMPATIBLE"},
		{"one unknown range permits explicit range", func(s, p *Signal) { p.MinParticipants, p.MaxParticipants = 4, 6 }, true, "PARTICIPANT_RANGE_COMPATIBLE"},
		{"partial contradictory bounds reject", func(s, p *Signal) { s.MinParticipants, p.MaxParticipants = 8, 7 }, false, ""},
		{"negative range rejects", func(s, p *Signal) { p.MinParticipants = -1 }, false, ""},
		{"reversed range rejects", func(s, p *Signal) { s.MinParticipants, s.MaxParticipants = 5, 4 }, false, ""},
		{"out of domain range rejects", func(s, p *Signal) { p.MaxParticipants = 101 }, false, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, peer := onlineSignals()
			test.change(&source, &peer)
			candidate, ok := Match(source, peer)
			if ok != test.want {
				t.Fatalf("Match() ok=%v want=%v candidate=%+v", ok, test.want, candidate)
			}
			if !ok {
				return
			}
			if candidate.SourceIntentID != strings.TrimSpace(source.IntentID) ||
				candidate.CandidateIntentID != strings.TrimSpace(peer.IntentID) ||
				candidate.AccountID != strings.TrimSpace(peer.AccountID) {
				t.Fatal("matching invented or swapped identities")
			}
			if len(candidate.ReasonCodes) != len(candidate.Reasons) || len(candidate.Reasons) < 2 {
				t.Fatal("matching explanations must have paired stable codes")
			}
			if test.code != "" && !containsCode(candidate.ReasonCodes, test.code) {
				t.Fatalf("missing reason %s: %+v", test.code, candidate)
			}
			if source.MinParticipants == 0 && source.MaxParticipants == 0 &&
				peer.MinParticipants == 0 && peer.MaxParticipants == 0 &&
				containsCode(candidate.ReasonCodes, "PARTICIPANT_RANGE_COMPATIBLE") {
				t.Fatal("unknown participant count claimed as matched")
			}
		})
	}
}

func containsCode(codes []string, code string) bool {
	for _, item := range codes {
		if item == code {
			return true
		}
	}
	return false
}

func TestBuildResponseStableDedupAndLimit(t *testing.T) {
	source, peer := onlineSignals()
	peers := make([]Signal, 0, 110)
	for i := 54; i >= 0; i-- {
		item := peer
		item.AccountID = fmt.Sprintf("person-%03d", i)
		item.IntentID = fmt.Sprintf("intent-%03d-b", i)
		peers = append(peers, item)
		item.IntentID = fmt.Sprintf("intent-%03d-a", i)
		peers = append(peers, item)
	}
	response := BuildResponse(source, peers)
	if response.Source != "RULE_BASED" || response.RuleVersion != "v1" ||
		response.SourceIntentID != source.IntentID || len(response.Candidates) != 50 || !response.Truncated {
		t.Fatalf("unexpected response: %+v", response)
	}
	for i, candidate := range response.Candidates {
		if candidate.AccountID != fmt.Sprintf("person-%03d", i) ||
			candidate.CandidateIntentID != fmt.Sprintf("intent-%03d-a", i) {
			t.Fatalf("unstable selection at %d: %+v", i, candidate)
		}
	}
	for i, j := 0, len(peers)-1; i < j; i, j = i+1, j-1 {
		peers[i], peers[j] = peers[j], peers[i]
	}
	if reverse := BuildResponse(source, peers); !reflect.DeepEqual(reverse, response) {
		t.Fatal("candidate response depends on input order")
	}
	duplicate := make([]Signal, 60)
	for i := range duplicate {
		duplicate[i] = peer
	}
	if one := BuildResponse(source, duplicate); len(one.Candidates) != 1 || one.Truncated {
		t.Fatalf("duplicate intents counted as new people: %+v", one)
	}
	if fifty := BuildResponse(source, peers[:100]); len(fifty.Candidates) != 50 || fifty.Truncated {
		t.Fatalf("exactly 50 unique candidates marked truncated: %+v", fifty)
	}
}

func TestCandidateJSONContainsOnlyMinimumPublicFields(t *testing.T) {
	source, peer := onlineSignals()
	source.Modality, peer.Modality = "HYBRID", "HYBRID"
	source.CityID, peer.CityID = "private-scope-city", "private-scope-city"
	source.AreaLabel, peer.AreaLabel = "source-sensitive-region", "source-sensitive-region"
	source.OnlinePlatform, peer.OnlinePlatform = "contact-sensitive-platform", "contact-sensitive-platform"
	response := BuildResponse(source, []Signal{peer})
	if len(response.Candidates) != 1 {
		t.Fatal("fixture did not produce candidate")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"private-scope-city", "source-sensitive-region", "contact-sensitive-platform",
		"placeId", "cityId", "areaLabel", "onlinePlatform", "contextId", "inviteeAccountIds",
		"latitude", "longitude", "chat", "body", "minParticipants", "maxParticipants",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private/raw matching field appeared in JSON: %s: %s", forbidden, encoded)
		}
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 5 {
		t.Fatalf("unexpected response keys: %+v", raw)
	}
	candidate := raw["candidates"].([]any)[0].(map[string]any)
	keys := []string{"sourceIntentId", "candidateIntentId", "accountId", "displayName", "category", "modality", "reasonCodes", "reasons"}
	if len(candidate) != len(keys) {
		t.Fatalf("unexpected candidate keys: %+v", candidate)
	}
	for _, key := range keys {
		if _, ok := candidate[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if !strings.Contains(string(encoded), "不代表距离或所在地") {
		t.Fatal("coarse scope explanation could imply location or distance")
	}
}

func TestEmptyResponseIsAnArrayAndFiltersIncompatibleSupply(t *testing.T) {
	source, peer := onlineSignals()
	peer.Category = "incompatible category"
	response := BuildResponse(source, []Signal{source, peer})
	if len(response.Candidates) != 0 || response.Truncated || response.Candidates == nil {
		t.Fatalf("empty supply fabricated matches: %+v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"candidates":[]`) {
		t.Fatalf("empty array contract: %s", encoded)
	}
}
