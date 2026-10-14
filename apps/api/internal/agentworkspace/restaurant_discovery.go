package agentworkspace

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// RestaurantDiscovery identifies a public category query. It grants no model,
// private food-preference, retained-source or external publication permission.
const RestaurantDiscovery = "restaurant_discovery"

// ParseRestaurantDiscoveryQuery consumes a bounded whole request rather than
// searching for the sentence as a literal venue name. Quality words express the
// user's request; they are not evidence that any returned restaurant is good.
// Unconsumed names, cities, cuisines, times or actions stay on the existing
// paths, so this public query cannot silently discard an additional constraint.
func ParseRestaurantDiscoveryQuery(query string) (MVPIntent, bool) {
	if !utf8.ValidString(query) || len(query) > 240 {
		return MVPIntent{}, false
	}
	for _, r := range query {
		if unicode.IsControl(r) {
			return MVPIntent{}, false
		}
	}
	text := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return MVPIntent{}, false
	}
	location, ok := chineseRestaurantDiscovery(text)
	if !ok {
		location, ok = englishRestaurantDiscovery(text)
	}
	if !ok {
		return MVPIntent{}, false
	}
	return MVPIntent{Operation: FindPlace, Target: FindPlace, Category: "restaurant",
		LocationPreference: location, Supported: true, Reason: RestaurantDiscovery}, true
}

func chineseRestaurantDiscovery(text string) (string, bool) {
	text = restaurantTrimPrefix(text, "能不能", "你能", "可以", "请", "麻烦")
	location := "city"
	var scope string
	text, scope = chineseRestaurantScope(text)
	if scope != "" {
		location = scope
	}
	text = restaurantTrimPrefix(text,
		"给我推荐一下", "帮我推荐一下", "帮我找一下", "给我推荐", "帮我推荐",
		"有没有", "有什么", "有哪些", "哪里有", "推荐一下", "找一下", "帮我找", "能推荐", "推荐", "查找", "搜索", "寻找", "找")
	var innerScope string
	text, innerScope = chineseRestaurantScope(text)
	if innerScope != "" {
		if scope != "" { // Duplicate or conflicting scopes are not dropped.
			return "", false
		}
		location = innerScope
	}
	text = restaurantTrimPrefix(text, "一些", "几家", "一家", "几个", "一个", "什么")
	text = restaurantTrimPrefix(text, "值得推荐的", "值得推荐", "比较好的", "推荐的", "好吃的", "不错的", "好的", "好吃", "不错", "好")
	for _, category := range []string{"餐厅", "餐馆"} {
		if tail, matched := strings.CutPrefix(text, category); matched {
			switch strings.TrimSpace(tail) {
			case "", "吗", "么", "呢", "推荐", "推荐一下", "可以推荐吗", "能推荐吗":
				return location, true
			}
		}
	}
	return "", false
}

func chineseRestaurantScope(text string) (string, string) {
	for _, scope := range []struct{ text, location string }{
		{"整个城市", "city"}, {"当前城市", "city"}, {"全城", "city"}, {"全市", "city"},
		{"这附近", "viewport"}, {"附近", "viewport"}, {"周边", "viewport"},
	} {
		if tail, ok := strings.CutPrefix(text, scope.text); ok {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tail), "的")), scope.location
		}
	}
	return text, ""
}

func englishRestaurantDiscovery(text string) (string, bool) {
	text = restaurantTrimPrefix(text, "please ")
	location, scope := "city", false
	for _, suffix := range []struct{ text, location string }{
		{" in the whole city", "city"}, {" in this city", "city"}, {" citywide", "city"},
		{" around here", "viewport"}, {" around me", "viewport"}, {" near me", "viewport"}, {" nearby", "viewport"},
	} {
		if head, ok := strings.CutSuffix(text, suffix.text); ok {
			text, location, scope = strings.TrimSpace(head), suffix.location, true
			break
		}
	}
	text = restaurantTrimPrefix(text,
		"can you recommend ", "could you recommend ", "can you suggest ", "could you suggest ",
		"can you find ", "help me find ", "what are some ", "are there any ", "are there ", "is there ",
		"do you know any ", "looking for ", "recommend ", "suggest ", "find ", "search for ", "what ", "any ")
	if tail, ok := strings.CutPrefix(text, "nearby "); ok {
		if scope {
			return "", false
		}
		text, location = strings.TrimSpace(tail), "viewport"
	}
	text = restaurantTrimPrefix(text, "some ", "any ", "a ")
	text = restaurantTrimPrefix(text, "good ", "nice ", "great ", "recommended ")
	switch text {
	case "restaurant", "restaurants", "restaurant recommendations", "restaurants you recommend", "restaurants do you recommend":
		return location, true
	}
	return "", false
}

func restaurantTrimPrefix(text string, prefixes ...string) string {
	for _, prefix := range prefixes {
		if tail, ok := strings.CutPrefix(text, prefix); ok {
			return strings.TrimSpace(tail)
		}
	}
	return text
}
