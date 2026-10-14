package agentworkspace

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// These are aliases of the original public Place taxonomy, not new catalog
// categories, activity types, named venues or personal location permissions.
var closedPlaceCategories = []struct {
	code             string
	english, chinese []string
}{
	{"restaurant", []string{"restaurant", "restaurants"}, []string{"餐馆", "餐厅"}},
	{"cafe", []string{"cafe", "cafes", "café", "cafés", "coffee shop", "coffee shops"}, []string{"咖啡馆", "咖啡店"}},
	{"museum", []string{"museum", "museums"}, []string{"博物馆"}},
	{"gallery", []string{"gallery", "galleries", "art gallery", "art galleries"}, []string{"美术馆", "画廊"}},
	{"library", []string{"library", "libraries"}, []string{"图书馆"}},
	{"park", []string{"park", "parks"}, []string{"公园"}},
}

func closedPlaceCategory(text string) string {
	for _, category := range closedPlaceCategories {
		for _, aliases := range [][]string{category.english, category.chinese} {
			for _, alias := range aliases {
				if text == alias {
					return category.code
				}
			}
		}
	}
	return ""
}

// Objective fields are consumed in full; unknown suffixes and duplicated or
// malformed clauses never become an empty search term. Recognition only fixes
// the existing FindPlace slots. The raw current question stays on that Task;
// native retained-field permission and the original reply/source fences still
// decide which facts may actually be read or answered.
func parseObjectivePlaceRefinement(query string) (string, bool) {
	category, _, ok := parseObjectivePlaceRequest(query)
	return category, ok
}

// ParseObjectivePlaceFields exposes only the already supported, fully consumed
// public field grammar. It carries no Place, source, scope or read authority.
func ParseObjectivePlaceFields(query string) ([]string, bool) {
	_, fields, ok := parseObjectivePlaceRequest(query)
	return fields, ok
}

func parseObjectivePlaceRequest(query string) (string, []string, bool) {
	if !utf8.ValidString(query) || len(query) > 240 {
		return "", nil, false
	}
	for _, r := range query {
		if unicode.IsControl(r) {
			return "", nil, false
		}
	}
	text := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	text = strings.Join(strings.Fields(text), " ")
	text = strings.ReplaceAll(text, "’", "'")
	text = strings.TrimPrefix(text, "please ")
	text = strings.TrimPrefix(text, "请")
	for _, prefix := range []struct {
		text    string
		english bool
	}{
		{"show ", true}, {"list ", true}, {"display ", true},
		{"显示", false}, {"列出", false}, {"给我看", false},
	} {
		rest, ok := strings.CutPrefix(text, prefix.text)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if prefix.english {
			rest = strings.TrimPrefix(rest, "the ")
		}
		for _, category := range closedPlaceCategories {
			aliases := category.chinese
			if prefix.english {
				aliases = category.english
			}
			for _, alias := range aliases {
				tail, matched := strings.CutPrefix(rest, alias)
				if !matched {
					continue
				}
				if prefix.english {
					// An explicit separator prevents a named venue or a longer
					// unknown category from being reduced to this public kind.
					separated := false
					for _, boundary := range []string{"'s ", "' ", " "} {
						if value, yes := strings.CutPrefix(tail, boundary); yes {
							tail, separated = value, true
							break
						}
					}
					if !separated {
						continue
					}
				} else {
					tail = strings.TrimPrefix(strings.TrimSpace(tail), "的")
				}
				if fields, yes := objectivePlaceFields(strings.TrimSpace(tail), prefix.english); yes {
					return category.code, fields, true
				}
			}
		}
		// A known request verb with an unconsumed object/field does not
		// permit dropping the remaining name, private or action words.
		return "", nil, false
	}
	return "", nil, false
}

func closedObjectivePlaceFields(text string, english bool) bool {
	_, ok := objectivePlaceFields(text, english)
	return ok
}

func objectivePlaceFields(text string, english bool) ([]string, bool) {
	var aliases map[string]string
	if english {
		text = strings.ReplaceAll(text, ", and ", ",")
		text = strings.ReplaceAll(text, " and ", ",")
		text = strings.ReplaceAll(text, "&", ",")
		aliases = map[string]string{
			"name": "name", "names": "name", "address": "address", "addresses": "address",
			"coordinate": "coordinates", "coordinates": "coordinates",
			"opening hour": PlaceOpeningHours, "opening hours": PlaceOpeningHours,
		}
	} else {
		text = strings.NewReplacer("、", ",", "，", ",", "和", ",", "与", ",", "及", ",").Replace(text)
		aliases = map[string]string{
			"名称": "name", "名字": "name", "店铺名称": "name", "地址": "address",
			"坐标": "coordinates", "经纬度": "coordinates",
			"营业时间": PlaceOpeningHours, "开放时间": PlaceOpeningHours,
		}
	}
	fields := strings.Split(text, ",")
	if len(fields) == 0 || len(fields) > 4 {
		return nil, false
	}
	seen := map[string]bool{}
	result := []string{}
	for _, field := range fields {
		canonical, known := aliases[strings.TrimSpace(field)]
		if !known || seen[canonical] {
			return nil, false
		}
		seen[canonical] = true
		result = append(result, canonical)
	}
	return result, len(seen) > 0
}
