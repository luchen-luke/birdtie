package agentworkspace

import (
	"strings"
	"testing"
)

func TestMVPIntentCorpus(t *testing.T) {
	previous := &Task{Intent: FindActivity, Filters: map[string]string{
		"category": "badminton", "timePreference": "weekend", "locationPreference": "city",
		"resultIDs": "activity-1,activity-2",
	}}
	for _, test := range []struct {
		name, query, operation, target, category, time, location, distance string
		current                                                            *Task
		supported                                                          bool
	}{
		{"周末羽毛球", "帮我找周末的羽毛球活动", FindActivity, FindActivity, "badminton", "weekend", "city", "", nil, true},
		{"今晚篮球", "今晚有篮球活动吗", FindActivity, FindActivity, "basketball", "tonight", "city", "", nil, true},
		{"附近运动", "附近的运动活动", FindActivity, FindActivity, "sports", "anytime", "viewport", "", nil, true},
		{"找组织", "找 CSSA 社团", FindOrganization, FindOrganization, "", "", "city", "", nil, true},
		{"找地点", "附近的图书馆地点", FindPlace, FindPlace, "", "", "viewport", "", nil, true},
		{"区域搜索", "搜索此区域", AreaDiscovery, FindActivity, "", "anytime", "viewport", "", nil, true},
		{"带上下文的区域搜索", "搜索此区域", AreaDiscovery, FindActivity, "badminton", "weekend", "viewport", "", previous, true},
		{"匿名合并区域搜索", "Find badminton this weekend. 搜索此区域", AreaDiscovery, FindActivity, "badminton", "weekend", "viewport", "", nil, true},
		{"上下文筛选", "近一点的呢？", RefineResults, FindActivity, "badminton", "weekend", "city", "closer", previous, true},
		{"匿名合并筛选", "Find badminton this weekend. Anything closer?", FindActivity, FindActivity, "badminton", "weekend", "city", "closer", nil, true},
		{"中文建议筛选", "Find badminton this weekend. 按市中心距离排序", FindActivity, FindActivity, "badminton", "weekend", "city", "closer", nil, true},
		{"比较结果", "比较前两个", CompareResults, FindActivity, "", "", "", "", previous, true},
		{"发布活动", "创建活动", CreateActivity, FindActivity, "", "anytime", "", "", nil, true},
		{"缺少上下文", "近一点的呢？", RefineResults, "", "", "", "", "", nil, false},
		{"未知", "帮我买飞机票", UnsupportedIntent, "", "", "", "", "", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ParseMVPIntent(test.query, test.current)
			if got.Operation != test.operation || got.Target != test.target ||
				got.Category != test.category || got.TimePreference != test.time ||
				got.LocationPreference != test.location || got.DistancePreference != test.distance ||
				got.Supported != test.supported {
				t.Fatalf("ParseMVPIntent(%q) = %#v", test.query, got)
			}
		})
	}
}

func TestPlaceTermKeepsMeaningfulName(t *testing.T) {
	got := ParseMVPIntent("Find a library place", nil)
	if got.Operation != FindPlace || got.SearchTerm != "library" {
		t.Fatalf("place term corrupted: %#v", got)
	}
}

func TestPlaceQueryScopeAndGeneratedSeparatorsDoNotBecomeNames(t *testing.T) {
	for _, query := range []string{"找地点", "地点", "查找地点", "搜索地点", "帮我找地点", "全城. 地点", "全城地点", "全市 地点", "整个城市 地点", "whole city find place", "citywide find place", "找地点. 地点"} {
		t.Run(query, func(t *testing.T) {
			got := ParseMVPIntent(query, nil)
			if !got.Supported || got.Operation != FindPlace || got.SearchTerm != "" || got.LocationPreference != "city" {
				t.Fatalf("place scope/term corrupted: %q => %#v", query, got)
			}
		})
	}
}

func TestPlaceNameKeepsInternalEnglishPeriodAndApostrophe(t *testing.T) {
	for _, query := range []string{"Find a St. Mary's library place", "全城. 找地点 St. Mary's library", `找地点 "St. Mary's library" 全城`} {
		got := ParseMVPIntent(query, nil)
		if !got.Supported || got.Operation != FindPlace || got.SearchTerm != "st. mary's library" {
			t.Fatalf("real place name changed: %q => %#v", query, got)
		}
	}
}

func TestQuotedPlaceValueCannotBecomeAnotherOperation(t *testing.T) {
	for _, term := range []string{"发布活动", "我的社交机会", "CSSA clubhouse", "认识新朋友"} {
		query := `找地点 "` + term + `" 附近`
		got := ParseMVPIntent(query, nil)
		if !got.Supported || got.Operation != FindPlace || got.SearchTerm != strings.ToLower(term) || got.LocationPreference != "viewport" {
			t.Fatalf("name value routed as another operation: %q => %#v", query, got)
		}
	}
	for _, query := range []string{`找地点 "library" 更近一点`, `找地点 "library" 发布活动`, `找地点 "unterminated`} {
		got := ParseMVPIntent(query, nil)
		if got.Supported || got.Operation != FindPlace {
			t.Fatalf("unavailable extra request silently discarded: %q => %#v", query, got)
		}
	}
}

func TestGoodPlaceStillDoesNotClaimRecommendations(t *testing.T) {
	got := ParseMVPIntent("好地点", nil)
	if !got.Supported || got.Operation != FindPlace || got.SearchTerm != "好" {
		t.Fatalf("qualifier silently invented as recommendation: %#v", got)
	}
}

func TestSportsVenueUsesPublicPlaceCategory(t *testing.T) {
	got := ParseMVPIntent("找羽毛球场馆", nil)
	if got.Operation != FindPlace || got.SearchTerm != "sports_venue" {
		t.Fatalf("sports venue routing = %#v", got)
	}
}

func TestAgentResultProjectionTypedQueryNamesRemainDistinct(t *testing.T) {
	for _, tc := range []struct{ query, operation, term string }{{"找公开成员", FindPerson, ""}, {"找公开成员 badminton", FindPerson, "badminton"}, {"找社区", FindCommunity, ""}, {"找商家", FindBusiness, ""}, {"我的社交机会", FindOpportunity, ""}} {
		got := ParseMVPIntent(tc.query, nil)
		if !got.Supported || got.Operation != tc.operation || got.SearchTerm != tc.term {
			t.Fatal(tc, got)
		}
	}
	if got := ParseMVPIntent("认识新朋友", nil); got.Operation != FindNewPeople {
		t.Fatal("new public-person query replaced original opt-in domain", got)
	}
}

func TestAgentResultProjectionOpportunityDoesNotDiscardRequestedFilters(t *testing.T) {
	for _, query := range []string{"我的社交机会今晚", "附近我的社交机会", "我的社交机会 badminton", "我的社交机会近一点", "my opportunities this weekend"} {
		got := ParseMVPIntent(query, nil)
		if got.Supported || got.Reason != "opportunity_filters_unavailable" {
			t.Fatalf("requested filter silently discarded: %q => %#v", query, got)
		}
	}
	for _, query := range []string{"帮我找我的社交机会", "my opportunities please", "我的社交机会？"} {
		if got := ParseMVPIntent(query, nil); !got.Supported || got.Operation != FindOpportunity || got.SearchTerm != "" {
			t.Fatalf("plain self query unavailable: %q => %#v", query, got)
		}
	}
}
