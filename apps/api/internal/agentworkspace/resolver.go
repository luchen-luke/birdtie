package agentworkspace

import "strings"

const FindActivity = "FIND_ACTIVITY"
const UnsupportedIntent = "UNSUPPORTED"

type IntentContext struct {
	Intent             string
	Category           string
	TimePreference     string
	DistancePreference string
	Supported          bool
}

// ResolveIntent recognizes one explicit MVP action and one contextual follow-up.
// It is deterministic so a future runtime can replace it without changing clients.
func ResolveIntent(query string, current *Task) IntentContext {
	text := strings.ToLower(strings.TrimSpace(query))
	if current != nil && current.Intent == FindActivity &&
		(strings.Contains(text, "closer") || strings.Contains(text, "更靠近市中心") || strings.Contains(text, "更近一点") || strings.Contains(text, "近一点") || strings.Contains(text, "按市中心距离排序")) {
		filters := current.Filters
		return IntentContext{
			Intent: FindActivity, Category: filters["category"],
			TimePreference: filters["timePreference"], DistancePreference: "closer", Supported: true,
		}
	}
	category := ""
	if strings.Contains(text, "badminton") || strings.Contains(text, "羽毛球") {
		category = "badminton"
	}
	hasActivityIntent := containsAny(text,
		"activity", "activities", "event", "events", "活动", "运动", "比赛", "查找", "找找", "搜索")
	if category == "" && !hasActivityIntent {
		return IntentContext{Intent: UnsupportedIntent}
	}
	timePreference := "anytime"
	if strings.Contains(text, "weekend") || strings.Contains(text, "saturday") || strings.Contains(text, "sunday") || strings.Contains(text, "周末") {
		timePreference = "weekend"
	}
	distancePreference := ""
	if strings.Contains(text, "closer") || strings.Contains(text, "更靠近市中心") || strings.Contains(text, "更近一点") || strings.Contains(text, "近一点") || strings.Contains(text, "按市中心距离排序") {
		distancePreference = "closer"
	}
	return IntentContext{Intent: FindActivity, Category: category, TimePreference: timePreference,
		DistancePreference: distancePreference, Supported: true}
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
