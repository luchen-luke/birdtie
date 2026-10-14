package agentworkspace

import (
	"context"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

// Optional native capability. Empty membership is an observed human search,
// not licensed place evidence, a model answer or a supplier/export permission.
type EmptyPlaceReplyRead interface{ Revalidate(context.Context) error }
type EmptyPlaceReplyResultsPort interface {
	TryOwnEmptyPlaceRefinementReply(context.Context, arp.Access) (Task, EmptyPlaceReplyRead, bool, error)
}

// This only recognizes bounded explicit slots; the native Store still verifies
// exact Task ownership, current session/policy and its original source read.
func IsExplicitPlaceRefinementTask(t Task) bool {
	if t.Status != TaskActive || t.PrincipalID == "" || t.ActingUserID != t.PrincipalID || t.Filters["reason"] != ExplicitPlaceRefinement || t.Filters["targetIntent"] != FindPlace || len(t.Conversation) == 0 {
		return false
	}
	query := t.Filters["currentQuery"]
	last := t.Conversation[len(t.Conversation)-1]
	if last.Role != "user" || last.Text != query || query == "" {
		return false
	}
	for _, key := range []string{"resultIDs", PlaceFollowupIDFilter, PlaceFollowupGenerationFilter, PlaceFollowupTopicFilter} {
		if _, exists := t.Filters[key]; exists {
			return false
		}
	}
	i, recognized := ParsePlaceRefinement(query, &t)
	if !recognized || !i.Supported || i.Operation != FindPlace || i.Reason != ExplicitPlaceRefinement || i.Category != t.Filters["category"] || i.SearchTerm != t.Filters["searchTerm"] || i.LocationPreference != t.Filters["locationPreference"] || i.TimePreference != t.Filters["timePreference"] || i.DistancePreference != t.Filters["distancePreference"] {
		return false
	}
	bounds, e := BoundsFromFilters(t.Filters)
	return e == nil && ((i.LocationPreference == "city" && bounds == nil) || (i.LocationPreference == "viewport" && bounds != nil))
}

// Call only after the original authorized native search proves zero members.
// It describes this bounded local set, never the existence of venues elsewhere.
func EmptyPlaceRefinementReply(t Task) string {
	category := map[string]string{"cafe": "咖啡馆", "restaurant": "餐馆", "museum": "博物馆", "gallery": "美术馆或画廊", "library": "图书馆", "park": "公园"}[t.Filters["category"]]
	if category == "" {
		category = "地点"
	}
	scope := "当前城市"
	if t.Filters["locationPreference"] == "viewport" {
		scope = "当前地图范围"
	}
	return scope + "内当前可用的站内地点没有符合筛选条件的" + category + "。可以调整类别或范围；本轮未调用联网搜索或模型。"
}
