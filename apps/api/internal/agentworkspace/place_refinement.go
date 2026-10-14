package agentworkspace

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const ExplicitPlaceRefinement = "explicit_place_refinement"

// Closed literal changes stay in the existing FindPlace path. RefineResults
// and CompareResults retain their activity-only contracts; this is neither a
// model tool request nor a location/private-context permission.
func ParsePlaceRefinement(query string, current *Task) (MVPIntent, bool) {
	if !utf8.ValidString(query) || len(query) > 240 {
		return MVPIntent{}, false
	}
	for _, r := range query {
		if unicode.IsControl(r) {
			return MVPIntent{}, false
		}
	}
	text := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	category, location, recognized := "", "", false
	for _, prefix := range []string{"只看", "只要", "改成", "改为", "换成", "only ", "change to "} {
		if tail, yes := strings.CutPrefix(text, prefix); yes {
			category = closedPlaceCategory(tail)
			if category != "" {
				recognized = true
				break
			}
		}
	}
	switch text {
	case "全城的呢", "改成全城", "改为全城", "只看全城", "whole city instead":
		location, recognized = "city", true
	case "只看当前地图范围", "改成当前地图范围", "当前地图范围内的呢", "current map area instead":
		location, recognized = "viewport", true
	}
	if !recognized {
		category, recognized = parseObjectivePlaceRefinement(query)
	}
	if !recognized {
		return MVPIntent{}, false
	}
	unavailable := MVPIntent{Operation: RefineResults, Reason: "previous_place_results_required"}
	if current == nil || current.Intent != FindPlace || current.PrincipalType != "person" || current.ContextType != "CITY" || current.CityID == "" || !oneOf(current.Status, TaskActive, TaskCompleted) || !oneOf(current.Filters["targetIntent"], "", FindPlace) {
		return unavailable, true
	}
	previous := current.Filters
	if !oneOf(previous["category"], "", "restaurant", "cafe", "museum", "gallery", "library", "park") || !oneOf(previous["locationPreference"], "city", "viewport") || !oneOf(previous["timePreference"], "", "anytime", "today", "tonight", "tomorrow", "weekend") || !oneOf(previous["distancePreference"], "", "closer") {
		return unavailable, true
	}
	term := previous["searchTerm"]
	if strings.TrimSpace(term) != term || !utf8.ValidString(term) || len(term) > 240 {
		return unavailable, true
	}
	for _, r := range term {
		if unicode.IsControl(r) {
			return unavailable, true
		}
	}
	if _, e := BoundsFromFilters(previous); e != nil {
		return MVPIntent{Operation: FindPlace, Reason: "map_bounds_required"}, true
	}
	intent := MVPIntent{Operation: FindPlace, Target: FindPlace, Category: previous["category"], TimePreference: previous["timePreference"], DistancePreference: previous["distancePreference"], SearchTerm: term, LocationPreference: previous["locationPreference"], Supported: true, Reason: ExplicitPlaceRefinement}
	if category != "" {
		// Changing kind is a new public search, not a renamed prior selection.
		intent.Category, intent.SearchTerm = category, ""
	}
	if location != "" {
		intent.LocationPreference = location
	}
	return intent, true
}
