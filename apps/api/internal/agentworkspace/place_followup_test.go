package agentworkspace

import (
	"reflect"
	"strings"
	"testing"
)

func placeFollowupTask() Task {
	return Task{Intent: FindPlace, Status: TaskCompleted, Filters: map[string]string{"targetIntent": FindPlace, "searchTerm": "Aberdeen Art Gallery", "locationPreference": "city"}, Conversation: []Message{{Role: "user", Text: "PRIVATE_HISTORY_CANARY"}, {Role: "assistant", Text: "PRIVATE_ASSISTANT_CANARY"}}}
}

func TestPlaceFollowupCurrentQuestionAndClosedSlots(t *testing.T) {
	for _, question := range []string{"它周末几点开门？", "这个地方周末开放时间？", "What time does it open this weekend?"} {
		t.Run(question, func(t *testing.T) {
			previous := placeFollowupTask()
			before := previous.Filters["searchTerm"]
			f, ok := ParsePlaceFollowup(question, &previous)
			intent := ParseMVPIntent(question, &previous)
			if !ok || !f.Pronoun || f.Name != "" || f.Topic != PlaceOpeningHours || f.TimePreference != "weekend" || !intent.Supported || intent.Operation != FindPlace || intent.Target != FindPlace || intent.SearchTerm != before || intent.TimePreference != "weekend" || previous.Filters["searchTerm"] != before {
				t.Fatal("place opening question did not preserve explicit native task slots", f, intent)
			}
		})
	}
	previous := placeFollowupTask()
	previous.Filters[PlaceFollowupTopicFilter], previous.Filters["timePreference"] = PlaceOpeningHours, "weekend"
	for _, question := range []string{"那 Maritime Museum 呢？", "What about Maritime Museum?"} {
		f, ok := ParsePlaceFollowup(question, &previous)
		if !ok || f.Pronoun || f.Name != "Maritime Museum" || f.Topic != PlaceOpeningHours || f.TimePreference != "weekend" {
			t.Fatal("explicit new public name lost inherited opening topic/weekend", f)
		}
		intent := ParseMVPIntent(question, &previous)
		if intent.Operation != FindPlace || intent.SearchTerm != "Maritime Museum" || intent.TimePreference != "weekend" {
			t.Fatal("named followup routed to unsupported/activity", intent)
		}
	}
	if f, ok := ParsePlaceFollowup("它今天几点开门？", &previous); !ok || f.TimePreference != "today" {
		t.Fatal("explicit current time did not replace the prior weekend", f)
	}
}

func TestPlaceFollowupDoesNotInferFromHistoryOrChangeOriginalRoutes(t *testing.T) {
	previous := placeFollowupTask()
	previous.Filters[PlaceFollowupTopicFilter] = PlaceOpeningHours
	for _, question := range []string{"发布活动", "我的关系信号", "找公开成员", "帮我买飞机票", `找地点 "Maritime Museum" 更近一点`, "它的私聊里说什么", "那 发布活动 呢？", "那 A\nB 呢？", "那 " + strings.Repeat("x", 170) + " 呢？"} {
		if _, ok := ParsePlaceFollowup(question, &previous); ok {
			t.Fatal("unrelated/private operation acquired a public place continuation", question)
		}
	}
	for _, current := range []*Task{nil, {Intent: FindActivity, Filters: map[string]string{PlaceFollowupTopicFilter: PlaceOpeningHours}}, {Intent: FindPlace, Filters: map[string]string{PlaceFollowupTopicFilter: "PRIVATE_UNSUPPORTED_TOPIC"}}} {
		if _, ok := ParsePlaceFollowup("它周末几点开门？", current); ok {
			t.Fatal("missing/other/invalid current place topic was accepted")
		}
	}
	if got := ParseMVPIntent(`找地点 "Maritime Museum" 全城`, &previous); !got.Supported || got.Operation != FindPlace || got.SearchTerm != "maritime museum" {
		t.Fatal("original quoted place route changed", got)
	}
	previous.Filters[PlaceFollowupTopicFilter], previous.Filters["timePreference"] = PlaceOpeningHours, "weekend"
	if f, ok := ParsePlaceFollowup("它不限时间的开放时间？", &previous); !ok || f.TimePreference != "anytime" {
		t.Fatal("explicit time reset silently retained weekend", f)
	}
}

func TestPlaceFollowupFiltersRemainPrivateAndExplicitClearPreservesTask(t *testing.T) {
	task := placeFollowupTask()
	task.Filters[PlaceFollowupIDFilter], task.Filters[PlaceFollowupGenerationFilter], task.Filters[PlaceFollowupTopicFilter] = "PRIVATE_ID_CANARY", "PRIVATE_GENERATION_CANARY", PlaceOpeningHours
	oldHistory := append([]Message(nil), task.Conversation...)
	public := SanitizeTaskForResponse(task)
	for _, key := range []string{PlaceFollowupIDFilter, PlaceFollowupGenerationFilter, PlaceFollowupTopicFilter} {
		if _, ok := public.Filters[key]; ok {
			t.Fatal("private native continuation proof became response data", key)
		}
	}
	ClearPlaceFollowupContext(&task)
	if len(task.Filters) != 3 || task.Filters["searchTerm"] != "Aberdeen Art Gallery" || !reflect.DeepEqual(oldHistory, task.Conversation) || task.Status != TaskCompleted {
		t.Fatal("clearing a retired entity proof reset unrelated task state")
	}
}
