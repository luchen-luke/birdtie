package agentworkspace

import (
	"encoding/json"
	"strings"
)

const (
	FindOrganization            = "FIND_ORGANIZATION"
	FindPlace                   = "FIND_PLACE"
	AreaDiscovery               = "AREA_DISCOVERY"
	RefineResults               = "REFINE_RESULTS"
	CompareResults              = "COMPARE_RESULTS"
	CreateActivity              = "CREATE_ACTIVITY"
	PersonalRelationshipContext = "PERSONAL_RELATIONSHIP_CONTEXT"
	FindNewPeople               = "FIND_NEW_PEOPLE"
	FindPerson                  = "FIND_PUBLIC_PERSON"
	FindCommunity               = "FIND_COMMUNITY"
	FindBusiness                = "FIND_BUSINESS"
	FindOpportunity             = "FIND_OWN_OPPORTUNITY"
)

// MVPIntent is an explicit operation and its normalized search slots. A
// recognized operation can still be unavailable without the needed context.
type MVPIntent struct {
	Operation          string `json:"operation"`
	Target             string `json:"target,omitempty"`
	Category           string `json:"category,omitempty"`
	TimePreference     string `json:"timePreference,omitempty"`
	LocationPreference string `json:"locationPreference,omitempty"`
	DistancePreference string `json:"distancePreference,omitempty"`
	SearchTerm         string `json:"searchTerm,omitempty"`
	Supported          bool   `json:"supported"`
	Reason             string `json:"reason,omitempty"`
}

func ParseMVPIntent(query string, current *Task) MVPIntent {
	text := strings.ToLower(strings.TrimSpace(query))
	if text == "" {
		return MVPIntent{Operation: UnsupportedIntent, Reason: "empty_query"}
	}
	if quoted, ok := quotedPlaceIntent(text); ok {
		return quoted
	}
	if containsAny(text, "找新朋友", "认识新朋友", "找伙伴", "找个伙伴", "找搭子", "meet new people", "find a companion") {
		return MVPIntent{Operation: FindNewPeople, Target: FindNewPeople, Supported: true}
	}
	// Explicit self queries only. No third-party relationship or chat-content tool.
	if containsAny(text, "我的关系信号", "我的好友互动", "我最近常和谁联系", "我经常和谁联系", "我常和谁协调", "my relationship signals", "who do i often coordinate with") {
		return MVPIntent{Operation: PersonalRelationshipContext, Target: PersonalRelationshipContext, Supported: true}
	}
	for _, entry := range []struct {
		operation string
		words     []string
	}{
		{FindOpportunity, []string{"我的社交机会", "我的活动机会", "适合我的机会", "my opportunities"}},
		{FindCommunity, []string{"社区", "社群", "community", "communities"}},
		{FindBusiness, []string{"商家", "商户", "business"}},
		{FindPerson, []string{"公开成员", "公开的人", "public people", "public person"}},
	} {
		if containsAny(text, entry.words...) {
			if entry.operation == FindOpportunity {
				// This rule generator uses the person's explicitly declared intent.
				// Extra query filters are not implemented; never silently drop them.
				words := append(append([]string{}, entry.words...), "帮我", "查找", "搜索", "找", "find", "search")
				remaining := strings.Trim(searchTerm(text, words...), " ?？!！。")
				if remaining != "" {
					return MVPIntent{Operation: FindOpportunity, Target: FindOpportunity, Reason: "opportunity_filters_unavailable"}
				}
				return MVPIntent{Operation: FindOpportunity, Target: FindOpportunity, LocationPreference: "city", Supported: true}
			}
			words := append(append([]string{}, entry.words...), "帮我", "查找", "搜索", "找", "附近", "公开", "的", "find", "search")
			term := searchTerm(text, words...)
			return MVPIntent{Operation: entry.operation, Target: entry.operation, SearchTerm: term, LocationPreference: "city", Supported: true}
		}
	}
	category := activityCategory(text)
	timePreference := activityTime(text)
	location := "city"
	if containsAny(text, "附近", "周边", "nearby", "near me", "around me") {
		location = "viewport"
	}
	if containsAny(text, "比较", "对比", "哪个更", "compare", "which is better") {
		if current == nil || baseIntent(current) != FindActivity || current.Filters["resultIDs"] == "" {
			return MVPIntent{Operation: CompareResults, Reason: "previous_results_required"}
		}
		return MVPIntent{Operation: CompareResults, Target: baseIntent(current), Supported: true}
	}
	if containsAny(text, "近一点", "更近", "靠近市中心", "按市中心距离排序", "closer", "换一批", "筛选", "refine") {
		if current == nil || baseIntent(current) != FindActivity {
			if category != "" || containsAny(text, "活动", "event", "activity", "activities") {
				return MVPIntent{Operation: FindActivity, Target: FindActivity,
					Category: category, TimePreference: timePreference,
					LocationPreference: location, DistancePreference: "closer", Supported: true}
			}
			return MVPIntent{Operation: RefineResults, Reason: "previous_results_required"}
		}
		previous := current.Filters
		intent := MVPIntent{
			Operation: RefineResults, Target: baseIntent(current),
			Category: previous["category"], TimePreference: previous["timePreference"],
			LocationPreference: previous["locationPreference"], Supported: true,
		}
		if containsAny(text, "近一点", "更近", "靠近市中心", "按市中心距离排序", "closer") {
			intent.DistancePreference = "closer"
		}
		if category != "" {
			intent.Category = category
		}
		if timePreference != "anytime" {
			intent.TimePreference = timePreference
		}
		return intent
	}
	if containsAny(text, "发布", "创建", "举办", "发起", "publish", "create", "host") &&
		containsAny(text, "活动", "比赛", "event", "activity", "tournament") {
		return MVPIntent{Operation: CreateActivity, Target: FindActivity,
			Category: category, TimePreference: timePreference, Supported: true}
	}
	if containsAny(text, "搜索此区域", "这片区域有什么", "这个区域有什么", "附近有什么", "周边有什么", "search this area", "what's nearby") {
		if current != nil && baseIntent(current) == FindActivity {
			if category == "" {
				category = current.Filters["category"]
			}
			if timePreference == "anytime" && current.Filters["timePreference"] != "" {
				timePreference = current.Filters["timePreference"]
			}
		}
		return MVPIntent{Operation: AreaDiscovery, Target: FindActivity,
			Category: category, TimePreference: timePreference,
			LocationPreference: "viewport", Supported: true}
	}
	if containsAny(text, "社团", "组织", "协会", "cssa", "society", "club", "organization") {
		return MVPIntent{Operation: FindOrganization, Target: FindOrganization,
			SearchTerm:         searchTerm(text, "找", "帮我", "查找", "搜索", "附近", "周边", "的", "公开", "社团", "组织", "协会", "society", "club", "organization", "find"),
			LocationPreference: location, Supported: true}
	}
	if containsAny(text, "地点", "场馆", "体育馆", "图书馆", "咖啡馆", "餐厅", "place", "venue", "library", "restaurant", "cafe") {
		term := placeSearchTerm(text)
		if containsAny(text, "体育馆", "运动场馆", "sports venue") ||
			(category != "" && containsAny(text, "场馆", "venue")) {
			term = "sports_venue"
		}
		return MVPIntent{Operation: FindPlace, Target: FindPlace,
			SearchTerm:         term,
			LocationPreference: location, Supported: true}
	}
	activity := category != "" || containsAny(text,
		"活动", "比赛", "运动", "event", "activity", "activities", "tournament", "play", "找找", "查找")
	if activity {
		return MVPIntent{Operation: FindActivity, Target: FindActivity,
			Category: category, TimePreference: timePreference,
			LocationPreference: location, Supported: true}
	}
	return MVPIntent{Operation: UnsupportedIntent, Reason: "unknown_intent"}
}

// Quoted place names remain search values even when they contain another
// operation's keywords. Only explicit scope words may follow a quoted name.
func quotedPlaceIntent(text string) (MVPIntent, bool) {
	remainder, ok := strings.CutPrefix(text, "找地点 ")
	if !ok || !strings.HasPrefix(strings.TrimSpace(remainder), `"`) {
		return MVPIntent{}, false
	}
	remainder = strings.TrimSpace(remainder)
	var term string
	decoder := json.NewDecoder(strings.NewReader(remainder))
	if err := decoder.Decode(&term); err != nil {
		return MVPIntent{Operation: FindPlace, Target: FindPlace, Reason: "invalid_place_name"}, true
	}
	tail := strings.TrimSpace(remainder[decoder.InputOffset():])
	remaining := searchTerm(tail, "整个城市", "whole city", "citywide", "全城", "全市", "near me", "around me", "nearby", "这附近", "附近", "周边", "的呢", "呢", "please")
	remaining = strings.Trim(remaining, " .?？!！。")
	if remaining != "" {
		return MVPIntent{Operation: FindPlace, Target: FindPlace, Reason: "place_followup_unavailable"}, true
	}
	location := "city"
	if containsAny(tail, "附近", "周边", "nearby", "near me", "around me") {
		location = "viewport"
	}
	return MVPIntent{Operation: FindPlace, Target: FindPlace, SearchTerm: term, LocationPreference: location, Supported: true}, true
}

func placeSearchTerm(text string) string {
	// Longer command/scope words must be consumed before their substrings.
	term := searchTerm(text, "帮我", "查找", "搜索", "找", "整个城市", "whole city", "citywide", "全城", "全市", "near me", "around me", "nearby", "附近", "周边", "一个", "地点", "场馆", "place", "venue", "find", "的")
	parts := make([]string, 0)
	for _, part := range strings.Fields(term) {
		part = strings.Trim(part, "?？!！。")
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

func baseIntent(task *Task) string {
	if task == nil {
		return ""
	}
	if task.Intent == RefineResults || task.Intent == CompareResults {
		return task.Filters["targetIntent"]
	}
	if task.Intent == AreaDiscovery {
		return FindActivity
	}
	return task.Intent
}

func activityCategory(text string) string {
	for _, item := range []struct{ code, chinese, english string }{
		{"badminton", "羽毛球", "badminton"},
		{"basketball", "篮球", "basketball"},
		{"football", "足球", "football"},
		{"sports", "运动", "sports"},
		{"culture", "文化", "culture"},
	} {
		if strings.Contains(text, item.chinese) || strings.Contains(text, item.english) {
			return item.code
		}
	}
	return ""
}

func activityTime(text string) string {
	switch {
	case containsAny(text, "今晚", "tonight"):
		return "tonight"
	case containsAny(text, "明天", "tomorrow"):
		return "tomorrow"
	case containsAny(text, "今天", "today"):
		return "today"
	case containsAny(text, "周末", "weekend", "saturday", "sunday"):
		return "weekend"
	default:
		return "anytime"
	}
}

func searchTerm(text string, words ...string) string {
	for _, word := range words {
		text = strings.ReplaceAll(text, word, " ")
	}
	terms := make([]string, 0)
	for _, word := range strings.Fields(strings.TrimSpace(text)) {
		if word != "a" && word != "an" && word != "the" && word != "me" && word != "please" {
			terms = append(terms, word)
		}
	}
	return strings.Join(terms, " ")
}
