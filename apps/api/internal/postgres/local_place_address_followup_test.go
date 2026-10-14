package postgres

import (
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

func localAddressFollowupTestTask() agentworkspace.Task {
	q := "地址"
	return agentworkspace.Task{Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, ContextType: "CITY", CityID: "synthetic-city", Filters: map[string]string{
		"currentQuery": q, "searchTerm": "Synthetic Glenhouse", "targetIntent": agentworkspace.FindPlace,
		agentworkspace.PlaceFollowupIDFilter:         "33333333-3333-4333-8333-333333333333",
		agentworkspace.PlaceFollowupGenerationFilter: strings.Repeat("a", 64), agentworkspace.PlaceFollowupTopicFilter: agentworkspace.PlaceAddress,
	}, Conversation: []agentworkspace.Message{{Role: "user", Text: "TEST_ONLY dinner"}, {Role: "assistant", Text: "TEST_ONLY hours"}, {Role: "user", Text: q}}}
}

func TestNowLocalPlaceAddressFollowupUsesOnlyTheCurrentFullyBoundNativeTask(t *testing.T) {
	task := localAddressFollowupTestTask()
	if q, ok := currentLocalPlaceQuestion(task); !ok || q.Name != "Synthetic Glenhouse" || q.Field != agentworkspace.PlaceAddress || q.Recommendation {
		t.Fatal("current bound address unavailable", q, ok)
	}
	for _, test := range []struct {
		name string
		edit func(*agentworkspace.Task)
	}{
		{"no_id", func(t *agentworkspace.Task) { delete(t.Filters, agentworkspace.PlaceFollowupIDFilter) }},
		{"bad_id", func(t *agentworkspace.Task) { t.Filters[agentworkspace.PlaceFollowupIDFilter] = "bad" }},
		{"no_generation", func(t *agentworkspace.Task) { delete(t.Filters, agentworkspace.PlaceFollowupGenerationFilter) }},
		{"zero_generation", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("0", 64)
		}},
		{"uppercase_generation", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("A", 64)
		}},
		{"hours_topic", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupTopicFilter] = agentworkspace.PlaceOpeningHours
		}},
		{"transport_topic", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupTopicFilter] = agentworkspace.PlaceTransport
		}},
		{"different_intent", func(t *agentworkspace.Task) { t.Intent = agentworkspace.FindActivity }},
		{"different_target", func(t *agentworkspace.Task) { t.Filters["targetIntent"] = agentworkspace.FindOrganization }},
		{"completed", func(t *agentworkspace.Task) { t.Status = agentworkspace.TaskCompleted }},
		{"private_context", func(t *agentworkspace.Task) { t.ContextType = "PERSON" }},
		{"no_city", func(t *agentworkspace.Task) { t.CityID = "" }},
		{"no_name", func(t *agentworkspace.Task) { t.Filters["searchTerm"] = "" }},
		{"control_name", func(t *agentworkspace.Task) { t.Filters["searchTerm"] = "Synthetic\nprivate" }},
		{"no_turn", func(t *agentworkspace.Task) { t.Conversation = nil }},
		{"assistant_turn", func(t *agentworkspace.Task) { t.Conversation[len(t.Conversation)-1].Role = "assistant" }},
		{"different_query_turn", func(t *agentworkspace.Task) { t.Conversation[len(t.Conversation)-1].Text = "other query" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := localAddressFollowupTestTask()
			test.edit(&task)
			if q, ok := currentLocalPlaceQuestion(task); ok {
				t.Fatal("unbound or stale address filter became a native answer", q)
			}
		})
	}
}

func TestNowLocalPlaceAddressFollowupDoesNotReviveOldTopicForBroaderOrPrivateQuestion(t *testing.T) {
	for _, query := range []string{"地址和路线", "它怎么去？", "What is its address and directions?", "我家的地址", "朋友的地址", "它的私人地址", "它的营业时间", "现在开门吗", "What are its opening hours?"} {
		t.Run(query, func(t *testing.T) {
			task := localAddressFollowupTestTask()
			task.Filters["currentQuery"] = query
			task.Conversation[len(task.Conversation)-1].Text = query
			if q, ok := currentLocalPlaceQuestion(task); ok {
				t.Fatal("old address topic became an unsupported fact answer", q)
			}
		})
	}
	task := localAddressFollowupTestTask()
	task.Filters["currentQuery"] = "What is the address of Maritime Museum?"
	task.Conversation[len(task.Conversation)-1].Text = task.Filters["currentQuery"]
	if q, ok := currentLocalPlaceQuestion(task); !ok || q.Name != "Maritime Museum" || q.Name == task.Filters["searchTerm"] {
		t.Fatal("literal replacement inherited Glenhouse", q, ok)
	}
}
