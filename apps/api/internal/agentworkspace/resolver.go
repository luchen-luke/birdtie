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
	if current != nil && current.Intent == FindActivity {
		filters := current.Filters
		preference := filters["distancePreference"]
		if strings.Contains(text, "closer") || strings.Contains(text, "nearer") {
			preference = "closer"
		}
		if strings.Contains(text, "farther") || strings.Contains(text, "further") {
			preference = ""
		}
		if filters["category"] != "" {
			// A short follow-up inherits the active search filters instead of
			// being reinterpreted as a brand-new unsupported request.
			return IntentContext{
				Intent: FindActivity, Category: filters["category"],
				TimePreference: filters["timePreference"], DistancePreference: preference, Supported: true,
			}
		}
	}
	if !strings.Contains(text, "badminton") {
		return IntentContext{Intent: UnsupportedIntent}
	}
	if !(strings.Contains(text, "find") || strings.Contains(text, "play") || strings.Contains(text, "activity") || strings.Contains(text, "badminton")) {
		return IntentContext{Intent: UnsupportedIntent}
	}
	timePreference := "anytime"
	if strings.Contains(text, "weekend") || strings.Contains(text, "saturday") || strings.Contains(text, "sunday") {
		timePreference = "weekend"
	}
	distancePreference := ""
	if strings.Contains(text, "closer") {
		distancePreference = "closer"
	}
	return IntentContext{Intent: FindActivity, Category: "badminton", TimePreference: timePreference,
		DistancePreference: distancePreference, Supported: true}
}
