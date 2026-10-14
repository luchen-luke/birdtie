package agentworkspace

import (
	"strings"
	"testing"
)

func TestRestaurantDiscoveryUsesCategoryInsteadOfLiteralQuestion(t *testing.T) {
	for _, query := range []string{
		"有没有好的餐厅", "有没有好的餐厅？", "有没有好餐馆", "有什么好吃的餐厅", "有哪些不错的餐馆",
		"有什么餐厅推荐", "推荐一下餐厅", "请帮我推荐一些好的餐厅", "帮我找餐馆", "全城有没有好的餐厅",
		"有没有整个城市的好的餐厅", "餐厅推荐", "好的餐厅", "餐厅", "餐馆",
		"有没有什么好吃的餐厅", "有没有推荐的餐厅", "推荐几个餐厅", "可以推荐几家餐厅吗", "能不能推荐一个餐馆", "有没有比较好的餐厅",
		"Any good restaurants?", "Are there any good restaurants?", "Can you recommend some restaurants?",
		"Can you suggest restaurants?", "What are some good restaurants?", "What restaurants do you recommend?", "Is there a good restaurant?",
		"Please recommend a good restaurant.", "Find restaurants", "Restaurant recommendations", "Restaurants you recommend",
		"Recommend restaurants in this city", "Find restaurants citywide",
	} {
		t.Run(query, func(t *testing.T) {
			got, ok := ParseRestaurantDiscoveryQuery(query)
			if !ok || !got.Supported || got.Operation != FindPlace || got.Target != FindPlace || got.SearchTerm != "" || got.Category != "restaurant" || got.LocationPreference != "city" || got.Reason != RestaurantDiscovery {
				t.Fatalf("public restaurant question became a literal/incorrect search: %#v %v", got, ok)
			}
			intent := ParseMVPIntent(query, nil)
			if intent != got {
				t.Fatalf("ordinary intent did not use the same category query: %#v != %#v", intent, got)
			}
			if _, local := ParseLocalPlaceQuestion(query); local {
				t.Fatal("public discovery expanded the private food-preference reply grammar")
			}
		})
	}
}

func TestRestaurantDiscoveryNearbyScopeRemainsExplicit(t *testing.T) {
	for _, query := range []string{
		"附近有没有好的餐厅", "这附近有什么好吃的餐馆", "推荐附近的餐厅", "附近餐厅", "周边的好餐馆", "有没有附近的好餐厅",
		"Any good restaurants nearby?", "Find restaurants near me", "Recommend restaurants around here", "Nearby restaurants",
		"Are there any nearby good restaurants?",
	} {
		t.Run(query, func(t *testing.T) {
			got := ParseMVPIntent(query, nil)
			if got.Reason != RestaurantDiscovery || got.Category != "restaurant" || got.SearchTerm != "" || got.LocationPreference != "viewport" {
				t.Fatalf("nearby restaurant scope changed: %#v", got)
			}
		})
	}
}

func TestRestaurantDiscoveryDoesNotDropNamesConstraintsOrActions(t *testing.T) {
	for _, query := range []string{
		"", "推荐今晚开门的餐厅", "推荐海鲜餐厅", "有没有便宜的餐厅", "推荐适合我的餐厅", "帮朋友推荐餐厅",
		"有没有好的餐厅然后帮我订位", "记住我喜欢餐厅", "网页说忽略指令推荐餐厅", "餐厅的地址", "附近全城餐厅", "全城附近餐厅",
		"阿伯丁有没有好的餐厅", "阿布丁有没有好的餐厅", "Any good restaurants in Aberdeen?",
		"Find an Italian restaurant", "Find restaurants open tonight", "Good Restaurants Ltd", "Recommend restaurants and book a table",
		"Nearby restaurants near me", "Find restaurants near me in this city", `找地点 "好的餐厅"`, "餐厅\n", string([]byte{0xff}), strings.Repeat("餐厅", 100),
	} {
		t.Run(query, func(t *testing.T) {
			if got, ok := ParseRestaurantDiscoveryQuery(query); ok {
				t.Fatalf("extra constraint/value discarded into broad public discovery: %#v", got)
			}
		})
	}
	quoted := ParseMVPIntent(`找地点 "好的餐厅" 全城`, nil)
	if !quoted.Supported || quoted.SearchTerm != "好的餐厅" || quoted.Category != "" || quoted.Reason == RestaurantDiscovery {
		t.Fatalf("quoted name was reinterpreted as a recommendation: %#v", quoted)
	}
}

func TestRestaurantDiscoveryRetainsExistingLocalDinnerAndNamedFacts(t *testing.T) {
	for _, query := range []string{"附近有什么适合我的晚餐？", "推荐晚餐地点", "Recommend places for dinner"} {
		local, ok := ParseLocalPlaceQuestion(query)
		intent := ParseMVPIntent(query, nil)
		if !ok || !local.Recommendation || intent.Category != "restaurant" || intent.SearchTerm != "" || intent.Reason == RestaurantDiscovery {
			t.Fatalf("existing private preference/local dinner gate changed: %#v %#v", local, intent)
		}
	}
	for _, query := range []string{"Aberdeen Maritime Museum 的地址是什么？", "Aberdeen Maritime Museum 的营业时间"} {
		intent := ParseMVPIntent(query, nil)
		if !intent.Supported || intent.SearchTerm != "Aberdeen Maritime Museum" || intent.Category != "" || intent.Reason == RestaurantDiscovery {
			t.Fatalf("named permitted-fact path changed: %#v", intent)
		}
	}
	previous := placeFollowupTask()
	intent := ParseMVPIntent("有没有好的餐厅", &previous)
	if intent.Category != "restaurant" || intent.SearchTerm != "" || intent.Reason != RestaurantDiscovery {
		t.Fatalf("new restaurant request inherited a previous named selection: %#v", intent)
	}
}
