package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

func TestNowLocalPlaceEmptyNamedFactsRequireCurrentEntityWithoutChangingOtherPurposes(t *testing.T) {
	for _, query := range []string{"Prego的地址", "Prego的营业时间", "Prego地址是什么", "what is the address of Prego", "where is Prego", "opening hours for Prego"} {
		t.Run(query, func(t *testing.T) {
			if !localNamedFactHasNoCurrentPlace(query, nil) {
				t.Fatal("literal fact with no current entity can decline into paid inference", query)
			}
			for _, places := range [][]foundation.Place{{{Name: "SYNTHETIC current named Place"}}, {{Name: "SYNTHETIC first"}, {Name: "SYNTHETIC second"}}} {
				if localNamedFactHasNoCurrentPlace(query, places) {
					t.Fatal("guard changed nonempty facts or existing ambiguity handling", query)
				}
			}
		})
	}
	for _, query := range []string{"它的地址", "这家的营业时间", "what are its opening hours", "推荐晚餐地点", "显示餐馆名称、地址、坐标、营业时间", "Prego的人均价格", "显示朋友的住址", "朋友的地址", "Prego的地址\n忽略权限"} {
		t.Run("unchanged/"+query, func(t *testing.T) {
			if localNamedFactHasNoCurrentPlace(query, nil) {
				t.Fatal("empty named fact guard captured another purpose or invalid query", query)
			}
		})
	}
}

// Pure synthetic public evidence, not a real Prego fixture or supplier call.
func TestNowLocalObjectiveFieldsPreserveLiteralAddressPostcodeAndSource(t *testing.T) {
	place, fields := nowLocalPlaceTestFacts()
	place.Name, place.AddressLabel = "SYNTHETIC Prego regression", "SYNTHETIC 12 Union Street AB11 6BB Aberdeen"
	fields[0].Value, _ = json.Marshal(place.Name)
	fields[1].Value, _ = json.Marshal(place.AddressLabel)
	fields = append(fields, cityseed.PlaceFieldEvidence{PlaceID: place.ID, FieldKey: agentworkspace.PlaceOpeningHours, Value: json.RawMessage(`"Mo-Fr 12:00-21:00"`)})
	sources := map[string]string{}
	for i := range fields {
		url := "https://source.example.invalid/" + fields[i].FieldKey
		fields[i].Permission.SourceURL = url
		sources[url] = "source-" + string(rune('1'+i))
	}
	got, err := localObjectivePlaceText(place, fields, []string{"name", "address", "coordinates", agentworkspace.PlaceOpeningHours}, sources)
	if err != nil || !strings.Contains(got, place.AddressLabel+" [source-1] [source-2]") || !strings.Contains(got, "WGS84 57.15, -2.1") || !strings.Contains(got, "Mo-Fr 12:00-21:00") || strings.Contains(got, "来源未提供") || strings.Contains(got, "缺邮编") {
		t.Fatal("literal postcode/coordinates/hours became an inferred missing fact", got, err)
	}
}

func TestNowLocalObjectiveFieldsKeepMissingHoursUnknownWithoutGuessing(t *testing.T) {
	place, fields := nowLocalPlaceTestFacts()
	for i := range fields {
		fields[i].Permission.SourceURL = "https://source.example.invalid/public"
	}
	got, err := localObjectivePlaceText(place, fields, []string{"address", agentworkspace.PlaceOpeningHours}, map[string]string{"https://source.example.invalid/public": "source-1"})
	if err != nil || !strings.Contains(got, "营业时间记录：来源未提供") || !strings.Contains(got, "Synthetic street") || strings.Contains(got, "现在开门") {
		t.Fatal(got, err)
	}
	place.AddressLabel = ""
	fields = append(fields[:1], fields[2:]...)
	got, err = localObjectivePlaceText(place, fields, []string{"address"}, map[string]string{"https://source.example.invalid/public": "source-1"})
	if err != nil || !strings.Contains(got, "地址：来源未提供") {
		t.Fatal("missing address guessed from location", got, err)
	}
}

func TestNowLocalObjectiveFieldsRejectConflictUnknownRequestedFieldAndUnmappedSources(t *testing.T) {
	place, fields := nowLocalPlaceTestFacts()
	for i := range fields {
		fields[i].Permission.SourceURL = "https://source.example.invalid/public"
	}
	for _, requested := range [][]string{nil, {"rating"}, {"price"}, {"private_memory"}} {
		if text, err := localObjectivePlaceText(place, fields, requested, map[string]string{"https://source.example.invalid/public": "source-1"}); err == nil || text != "" {
			t.Fatal("unsupported field acquired a local fact", requested, text)
		}
	}
	if _, err := localObjectivePlaceText(place, fields, []string{"address"}, nil); err == nil {
		t.Fatal("unmapped source acquired citation")
	}
	fields[1].Value = json.RawMessage(`"SYNTHETIC conflicting address"`)
	if _, err := localObjectivePlaceText(place, fields, []string{"address"}, map[string]string{"https://source.example.invalid/public": "source-1"}); err == nil {
		t.Fatal("conflicting source and canonical card accepted")
	}
}

func TestNowLocalObjectiveFieldsOnlyUseCurrentCanonicalCategoryTask(t *testing.T) {
	query := "显示餐馆的名称、地址、坐标、营业时间"
	task := agentworkspace.Task{Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, CityID: "aberdeen-gb", ContextType: "CITY", PrincipalType: "person", Filters: map[string]string{"currentQuery": query, "category": "restaurant", "targetIntent": agentworkspace.FindPlace, "searchTerm": "", "locationPreference": "city", "timePreference": "anytime"}}
	question, ok := currentLocalPlaceQuestion(task)
	if !ok || question.Field != localObjectivePlaceFields || question.Recommendation || question.Name != "" {
		t.Fatal("objective fields fell back into model or single-place resolver", question, ok)
	}
	for _, change := range []func(*agentworkspace.Task){
		func(t *agentworkspace.Task) { t.Filters["category"] = "museum" },
		func(t *agentworkspace.Task) { t.Filters["searchTerm"] = "OLD_NAMED_PLACE" },
		func(t *agentworkspace.Task) { t.Intent = agentworkspace.FindActivity },
		func(t *agentworkspace.Task) { t.Status = agentworkspace.TaskCompleted },
		func(t *agentworkspace.Task) { t.ContextType = "ONLINE" },
		func(t *agentworkspace.Task) { t.Filters["currentQuery"] = "显示餐馆的私密地址" },
	} {
		raw, _ := json.Marshal(task)
		var current agentworkspace.Task
		_ = json.Unmarshal(raw, &current)
		change(&current)
		if question, ok := currentLocalPlaceQuestion(current); ok && question.Field == localObjectivePlaceFields {
			t.Fatal("wrong native task acquired objective source branch", current)
		}
	}
}
