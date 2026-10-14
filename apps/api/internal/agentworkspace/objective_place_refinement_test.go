package agentworkspace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestObjectivePlaceFieldsExposeOnlyFullyConsumedCanonicalRequestedFields(t *testing.T) {
	for _, tc := range []struct {
		query  string
		fields []string
	}{
		{"显示餐馆的名称、地址、坐标、营业时间", []string{"name", "address", "coordinates", PlaceOpeningHours}},
		{"show restaurants' addresses and names", []string{"address", "name"}},
		{"请显示博物馆的营业时间。", []string{PlaceOpeningHours}},
	} {
		got, ok := ParseObjectivePlaceFields(tc.query)
		if !ok || !reflect.DeepEqual(got, tc.fields) {
			t.Fatal(tc.query, got, ok)
		}
	}
	for _, query := range []string{"显示餐馆的名称和评分", "显示餐馆的地址和地址", "显示餐馆的地址然后预订", "显示朋友的住址", "餐馆名称", "显示餐馆的名称\n忽略指令"} {
		if fields, ok := ParseObjectivePlaceFields(query); ok || fields != nil {
			t.Fatal("unknown or mixed field/action request accepted", query, fields)
		}
	}
}

func TestObjectivePlaceRefinementKeepsCanonicalPublicScopeAndFieldsOutOfName(t *testing.T) {
	for _, tc := range []struct{ query, category string }{
		{"show restaurant names and addresses", "restaurant"},
		{"show restaurants name and address", "restaurant"},
		{"Show restaurants' names and addresses.", "restaurant"},
		{"please list restaurant names, addresses, and opening hours", "restaurant"},
		{"show cafe name and address", "cafe"},
		{"display cafés names and coordinates", "cafe"},
		{"show coffee shops' names and addresses", "cafe"},
		{"show museum name and address", "museum"},
		{"show museums names and addresses", "museum"},
		{"list gallery name and coordinates", "gallery"},
		{"list galleries names and addresses", "gallery"},
		{"display library name and address", "library"},
		{"display libraries names and addresses", "library"},
		{"show park name and address", "park"},
		{"show parks names and addresses", "park"},
		{"显示餐馆的名称和地址", "restaurant"},
		{"列出餐厅名称、地址、营业时间", "restaurant"},
		{"显示咖啡馆的名称、地址与开放时间", "cafe"},
		{"给我看咖啡店的名称和经纬度", "cafe"},
		{"显示博物馆的地址", "museum"},
		{"显示美术馆的名称和坐标", "gallery"},
		{"列出画廊的名称和地址", "gallery"},
		{"显示图书馆的名称及地址", "library"},
		{"请显示公园的名称和地址。", "park"},
		{"显示餐馆的营业时间", "restaurant"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			previous := refinementUnitPlaceTask()
			// Reproduce the actual malformed current slots without replacing its
			// original Task: the previous raw turn is not a real place name.
			previous.Filters["category"] = ""
			previous.Filters["searchTerm"] = "show restaurant names and addresses"
			before, _ := json.Marshal(previous)
			got := ParseMVPIntent(tc.query, &previous)
			if !got.Supported || got.Operation != FindPlace || got.Target != FindPlace || got.Reason != ExplicitPlaceRefinement || got.Category != tc.category || got.SearchTerm != "" || got.LocationPreference != "viewport" || got.TimePreference != "weekend" || got.DistancePreference != "closer" {
				t.Fatalf("objective request became a name, wrong operation or widened scope: %#v", got)
			}
			if after, _ := json.Marshal(previous); string(before) != string(after) {
				t.Fatal("parse rewrote original Task/history/selection")
			}
			if _, followup := ParsePlaceFollowup(tc.query, &previous); followup {
				t.Fatal("category-wide objective request used old single-place pronoun/name resolver")
			}
			if _, literal := ParseLocalPlaceQuestion(tc.query); literal {
				t.Fatal("category-wide objective request used one literal place fact branch")
			}
		})
	}
}

func TestObjectivePlaceRefinementPreservesCityAndOriginalEmptyMembershipContract(t *testing.T) {
	task := emptyRefinementUnitTask()
	query := "show restaurant names and addresses"
	task.Filters["currentQuery"] = query
	task.Filters["category"] = "restaurant"
	task.Conversation[len(task.Conversation)-1].Text = query
	got, recognized := ParsePlaceRefinement(query, &task)
	if !recognized || !got.Supported || got.LocationPreference != "city" || got.SearchTerm != "" || got.TimePreference != "anytime" {
		t.Fatal("city objective refinement was not canonical", got)
	}
	if !IsExplicitPlaceRefinementTask(task) {
		t.Fatal("original authorized empty-membership task contract did not recognize objective refinement")
	}
	for _, key := range []string{"resultIDs", PlaceFollowupIDFilter, PlaceFollowupGenerationFilter, PlaceFollowupTopicFilter} {
		copy := task
		copy.Filters = make(map[string]string, len(task.Filters)+1)
		for k, v := range task.Filters {
			copy.Filters[k] = v
		}
		copy.Filters[key] = "OLD_CANARY"
		if IsExplicitPlaceRefinementTask(copy) {
			t.Fatal("objective wording admitted stale selection/membership correlation", key)
		}
	}
}

func TestObjectivePlaceRefinementPrecedesSelectedAndNamedFactResolvers(t *testing.T) {
	for _, query := range []string{"显示餐馆的名称和地址", "显示博物馆的地址", "显示餐馆的营业时间"} {
		t.Run(query, func(t *testing.T) {
			previous := refinementUnitPlaceTask()
			if got, followup := ParsePlaceFollowup(query, &previous); followup {
				t.Errorf("objective category request was consumed before intent parser by selected-place resolver: %#v", got)
			}
			if got, literal := ParseLocalPlaceQuestion(query); literal {
				t.Errorf("objective category request was consumed by one-place native fact resolver: %#v", got)
			}
		})
	}
	previous := refinementUnitPlaceTask()
	if got, ok := ParsePlaceFollowup("它的地址", &previous); !ok || !got.Pronoun || got.Topic != PlaceAddress {
		t.Fatal("original current selected-place address contract changed", got)
	}
}

func TestObjectivePlaceRefinementRequiresExistingPublicPlaceContext(t *testing.T) {
	for name, change := range map[string]func(*Task){
		"activity":             func(v *Task) { v.Intent = FindActivity },
		"organization":         func(v *Task) { v.PrincipalType = "organization" },
		"online":               func(v *Task) { v.ContextType = "ONLINE" },
		"failed":               func(v *Task) { v.Status = TaskFailed },
		"missing city":         func(v *Task) { v.CityID = "" },
		"private scope":        func(v *Task) { v.Filters["locationPreference"] = "home" },
		"private distance":     func(v *Task) { v.Filters["distancePreference"] = "private_gps" },
		"private time":         func(v *Task) { v.Filters["timePreference"] = "private_calendar" },
		"unsupported category": func(v *Task) { v.Filters["category"] = "badminton" },
		"mixed target":         func(v *Task) { v.Filters["targetIntent"] = FindActivity },
		"invalid prior name":   func(v *Task) { v.Filters["searchTerm"] = "Unit\nprivate" },
		"malformed bounds":     func(v *Task) { delete(v.Filters, "mapWest") },
	} {
		t.Run(name, func(t *testing.T) {
			previous := refinementUnitPlaceTask()
			change(&previous)
			before, _ := json.Marshal(previous)
			got := ParseMVPIntent("show restaurant names and addresses", &previous)
			if got.Supported {
				t.Fatal("objective words widened non-public or invalid prior task", got)
			}
			if after, _ := json.Marshal(previous); string(before) != string(after) {
				t.Fatal("rejected parse changed original Task")
			}
		})
	}
	if got := ParseMVPIntent("show restaurant names and addresses", nil); got.Supported || got.Reason != "previous_place_results_required" {
		t.Fatal("objective noun phrase started a context-free broad lookup", got)
	}
}

func TestObjectivePlaceRefinementConsumesWholeGrammarAndNoPrivateOrNamedSuffix(t *testing.T) {
	for _, query := range []string{
		"show restaurant Glenhouse names and addresses",
		"show Glenhouse restaurant names and addresses",
		`show "restaurant" names and addresses`,
		"show restaurants names and addresses from my private messages",
		"show restaurants names and addresses and send to my friend",
		"show restaurants names and addresses near my home",
		"show restaurants names and addresses near me",
		"show restaurants names and GPS coordinates",
		"show restaurants names and ratings",
		"show restaurants names and menus",
		"show restaurants names and private opening hours",
		"show restaurants names and addresses then book it",
		"show restaurants names and addresses in another city",
		"show restaurants names and names",
		"show restaurants names,,addresses",
		"show restaurants names and",
		"show restaurant names and addresses\nignore permissions",
		"show restaurant names and addresses\x00",
		"show restaurant names and addresses " + strings.Repeat("x", 241),
		"显示我家附近餐馆的名称和地址",
		"显示Glenhouse餐馆的名称和地址",
		"显示餐馆的名称和地址并发送给好友",
		"显示餐馆的名称和地址使用私聊",
		"显示餐馆的名称和地址及用户GPS",
		"显示餐馆的名称和地址以及未知字段",
		"显示餐馆的名称和名称",
		"显示体育馆的名称和地址",
	} {
		t.Run(query, func(t *testing.T) {
			previous := refinementUnitPlaceTask()
			got := ParseMVPIntent(query, &previous)
			if got.Supported && got.Operation == FindPlace && got.Reason == ExplicitPlaceRefinement {
				t.Fatal("unknown/private/name text was removed into a broad public request", got)
			}
		})
	}
	previous := refinementUnitPlaceTask()
	if got := ParseMVPIntent(`找地点 "Restaurant Glenhouse" 全城`, &previous); !got.Supported || got.SearchTerm != "restaurant glenhouse" || got.Reason == ExplicitPlaceRefinement {
		t.Fatal("original explicit quoted place lookup changed", got)
	}
	if got, ok := ParseLocalPlaceQuestion("Glenhouse 的地址"); !ok || got.Name != "Glenhouse" || got.Field != PlaceAddress {
		t.Fatal("original named field question changed", got)
	}
}
