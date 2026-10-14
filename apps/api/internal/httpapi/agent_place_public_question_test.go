package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

// Registered HTTP boundary with native/model doubles only. No SQL, network
// search or provider call is represented by these tests.
func TestPlacePublicQuestionHTTPKeepsOriginalTaskAndEveryRawQuestion(t *testing.T) {
	_, mux, store, live, token := placeFollowupHTTPFixture(t)
	store.task.Filters["timePreference"] = "weekend"
	for index, sample := range []struct{ question, topic string }{
		{"它需要买票吗？", agentworkspace.PlaceAdmission},
		{"那里有轮椅入口吗？", agentworkspace.PlaceAccessibility},
		{"How do I get there?", agentworkspace.PlaceTransport},
		{"它适合带孩子去吗？", agentworkspace.PlacePublicInformation},
		{"What about Maritime Museum?", agentworkspace.PlacePublicInformation},
	} {
		live.expectedTopic = sample.topic
		body, _ := json.Marshal(map[string]string{"query": sample.question, "taskId": httpActionSafetyTaskID})
		status, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body))
		var response struct{ Data agentworkspace.Results }
		if status != http.StatusOK || json.Unmarshal(raw, &response) != nil || response.Data.Task == nil || store.calls != index+1 || live.sends != index+1 {
			t.Fatal("public followup missed original native or answer boundary", status)
		}
		got := response.Data
		if got.Task.ID != httpActionSafetyTaskID || got.Task.Status != agentworkspace.TaskCompleted || got.Mode != "live" || len(got.Task.Conversation) != 4+2*index || got.Task.Conversation[len(got.Task.Conversation)-2].Text != sample.question || live.seen[index].Filters[agentworkspace.PlaceFollowupTopicFilter] != sample.topic {
			t.Fatal("raw question, history, current topic or sourced completion lost")
		}
		if len(got.ResultSet.Items) != 1 || len(got.MapEffects.PinEntityIDs) != 1 || got.MapEffects.PinEntityIDs[0] != got.ResultSet.Items[0].Entity.Key() {
			t.Fatal("new question invented a different card/map subject")
		}
	}
}
