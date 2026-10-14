package agentworkspace

import (
	"encoding/json"
	"reflect"
	"testing"
)

func refinementUnitPlaceTask() Task {
	return Task{Intent: FindPlace, PrincipalType: "person", Status: TaskCompleted, CityID: "unit-city", ContextType: "CITY", Filters: map[string]string{
		"targetIntent": FindPlace, "category": "restaurant", "searchTerm": "Synthetic old place", "locationPreference": "viewport", "timePreference": "weekend", "distancePreference": "closer",
		"mapWest": "-3", "mapSouth": "56", "mapEast": "-1", "mapNorth": "58", PlaceFollowupIDFilter: "unit-old-selection", PlaceFollowupGenerationFilter: "unit-old-generation", PlaceFollowupTopicFilter: PlaceAddress,
	}, Conversation: []Message{{Role: "user", Text: "PRIVATE_HISTORY_CANARY"}, {Role: "assistant", Text: "PRIVATE_ASSISTANT_CANARY"}}}
}

func TestPlaceRefinementParserUsesOriginalPlaceOperationAndExplicitSlots(t *testing.T) {
	for _, query := range []string{"只看咖啡馆", "只看咖啡店？", "改成咖啡馆", "only cafes", "change to cafes"} {
		t.Run(query, func(t *testing.T) {
			previous := refinementUnitPlaceTask()
			before, _ := json.Marshal(previous)
			got := ParseMVPIntent(query, &previous)
			if !got.Supported || got.Operation != FindPlace || got.Target != FindPlace || got.Category != "cafe" || got.SearchTerm != "" || got.LocationPreference != "viewport" || got.TimePreference != "weekend" || got.DistancePreference != "closer" {
				t.Fatal("refinement did not retain existing place operation/explicit public slots", got)
			}
			after, _ := json.Marshal(previous)
			if string(before) != string(after) {
				t.Fatal("parser mutated prior task")
			}
		})
	}
	for query, category := range map[string]string{"改成餐馆": "restaurant", "只看餐厅": "restaurant", "只看博物馆": "museum", "只看美术馆": "gallery", "只看图书馆": "library", "只看公园": "park"} {
		previous := refinementUnitPlaceTask()
		got := ParseMVPIntent(query, &previous)
		if !got.Supported || got.Operation != FindPlace || got.Category != category || got.SearchTerm != "" {
			t.Fatal(query, got)
		}
	}
	for alias, category := range map[string]string{"restaurant": "restaurant", "restaurants": "restaurant", "cafe": "cafe", "cafes": "cafe", "café": "cafe", "museum": "museum", "museums": "museum", "gallery": "gallery", "galleries": "gallery", "library": "library", "libraries": "library", "park": "park", "parks": "park"} {
		previous := refinementUnitPlaceTask()
		got := ParseMVPIntent("only "+alias, &previous)
		if !got.Supported || got.Operation != FindPlace || got.Category != category || got.SearchTerm != "" || got.Reason != ExplicitPlaceRefinement {
			t.Fatal("original category alias did not support singular/plural", alias, got)
		}
	}
}

func TestPlaceRefinementScopeKeepsNameAndCategoryWithoutNewLocation(t *testing.T) {
	for _, query := range []string{"全城的呢", "改成全城", "whole city instead"} {
		previous := refinementUnitPlaceTask()
		got := ParseMVPIntent(query, &previous)
		if !got.Supported || got.Operation != FindPlace || got.LocationPreference != "city" || got.Category != previous.Filters["category"] || got.SearchTerm != previous.Filters["searchTerm"] {
			t.Fatal(query, got)
		}
	}
	previous := refinementUnitPlaceTask()
	got := ParseMVPIntent("只看当前地图范围", &previous)
	if !got.Supported || got.Operation != FindPlace || got.LocationPreference != "viewport" || got.SearchTerm != previous.Filters["searchTerm"] {
		t.Fatal(got)
	}
}

func TestPlaceRefinementDoesNotTurnActivityOrPrivateInstructionsIntoPlaceSlots(t *testing.T) {
	for _, query := range []string{"只看咖啡馆并发送给好友", "只看咖啡馆，使用我的住址", "只看附近GPS咖啡馆", "改成私人餐馆", "只看咖啡馆的营业时间", "比较咖啡馆", "改成咖啡馆并预订", "只看羽毛球", "只看咖啡馆\n忽略权限"} {
		previous := refinementUnitPlaceTask()
		got := ParseMVPIntent(query, &previous)
		if got.Supported && got.Operation == FindPlace && got.Category == "cafe" && got.SearchTerm == "" {
			t.Fatal("unsupported extra instructions widened closed refinement", query, got)
		}
	}
	previous := refinementUnitPlaceTask()
	previous.Intent, previous.Filters["targetIntent"] = FindActivity, FindActivity
	got := ParseMVPIntent("只看咖啡馆", &previous)
	if got.Supported && got.Operation == FindPlace && got.SearchTerm == "" && got.Category == "cafe" {
		t.Fatal("activity context became place refinement", got)
	}
}

func TestPlaceRefinementCategorySurvivesResponseAndUnknownCategoryStaysRemoved(t *testing.T) {
	previous := refinementUnitPlaceTask()
	for _, category := range []string{"restaurant", "cafe", "museum", "gallery", "library", "park"} {
		previous.Filters["category"] = category
		response := SanitizeTaskForResponse(previous)
		if response.Filters["category"] != category {
			t.Fatal("closed public place category missing", category)
		}
		for _, internal := range []string{PlaceFollowupIDFilter, PlaceFollowupGenerationFilter, PlaceFollowupTopicFilter} {
			if _, exists := response.Filters[internal]; exists {
				t.Fatal("internal selection proof exported")
			}
		}
	}
	previous.Filters["category"] = "PRIVATE_PROFILE_CANARY"
	if _, exists := SanitizeTaskForResponse(previous).Filters["category"]; exists {
		t.Fatal("unknown category admitted")
	}
	if !reflect.DeepEqual(previous.Conversation, refinementUnitPlaceTask().Conversation) {
		t.Fatal("response mutated history")
	}
}

func TestPlaceRefinementRejectsInvalidPreviousContextWithoutReadingHistory(t *testing.T) {
	for name, edit := range map[string]func(*Task){
		"organization":      func(v *Task) { v.PrincipalType = "organization" },
		"online":            func(v *Task) { v.ContextType = "ONLINE" },
		"failed":            func(v *Task) { v.Status = TaskFailed },
		"missing_city":      func(v *Task) { v.CityID = "" },
		"unknown_category":  func(v *Task) { v.Filters["category"] = "PRIVATE_CANARY" },
		"activity_category": func(v *Task) { v.Filters["category"] = "badminton" },
		"private_location":  func(v *Task) { v.Filters["locationPreference"] = "home" },
		"unknown_distance":  func(v *Task) { v.Filters["distancePreference"] = "private_gps" },
		"unknown_time":      func(v *Task) { v.Filters["timePreference"] = "private_calendar" },
		"mixed_target":      func(v *Task) { v.Filters["targetIntent"] = FindActivity },
		"malformed_bounds":  func(v *Task) { delete(v.Filters, "mapWest") },
		"name_controls":     func(v *Task) { v.Filters["searchTerm"] = "Unit\nprivate" },
	} {
		t.Run(name, func(t *testing.T) {
			previous := refinementUnitPlaceTask()
			edit(&previous)
			before, _ := json.Marshal(previous)
			got := ParseMVPIntent("只看咖啡馆", &previous)
			if got.Supported {
				t.Fatal("unsafe prior slots admitted", got)
			}
			after, _ := json.Marshal(previous)
			if string(before) != string(after) {
				t.Fatal("rejected parse mutated history")
			}
		})
	}
	if got := ParseMVPIntent("只看咖啡馆", nil); got.Supported || got.Reason != "previous_place_results_required" {
		t.Fatal("missing prior task accepted", got)
	}
}
