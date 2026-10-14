package agentworkspace

import "testing"

func emptyRefinementUnitTask() Task {
	return Task{ID: "49100000-0000-4000-8000-000000000002", PrincipalType: "person", PrincipalID: "49100000-0000-4000-8000-000000000001", ActingUserID: "49100000-0000-4000-8000-000000000001", ContextType: "CITY", CityID: "aberdeen-gb", Intent: FindPlace, Status: TaskActive, Query: "找地点", Filters: map[string]string{"targetIntent": FindPlace, "reason": ExplicitPlaceRefinement, "currentQuery": "only cafes", "category": "cafe", "locationPreference": "city", "searchTerm": "", "timePreference": "anytime"}, Conversation: []Message{{Role: "user", Text: "找地点"}, {Role: "assistant", Text: "UNIT old answer"}, {Role: "user", Text: "only cafes"}}}
}

func TestEmptyPlaceRefinementRequiresExactCurrentPublicSlots(t *testing.T) {
	task := emptyRefinementUnitTask()
	if !IsExplicitPlaceRefinementTask(task) {
		t.Fatal("closed current category rejected")
	}
	for name, change := range map[string]func(*Task){
		"activity":            func(t *Task) { t.Intent = FindActivity },
		"organization":        func(t *Task) { t.PrincipalType = "organization" },
		"other actor":         func(t *Task) { t.ActingUserID = "other" },
		"online":              func(t *Task) { t.ContextType = "ONLINE" },
		"completed":           func(t *Task) { t.Status = TaskCompleted },
		"missing city":        func(t *Task) { t.CityID = "" },
		"no current user":     func(t *Task) { t.Conversation[len(t.Conversation)-1].Role = "assistant" },
		"different raw user":  func(t *Task) { t.Conversation[len(t.Conversation)-1].Text = "只看咖啡馆" },
		"old reason":          func(t *Task) { t.Filters["reason"] = "local_place_question" },
		"old category":        func(t *Task) { t.Filters["category"] = "restaurant" },
		"old named selection": func(t *Task) { t.Filters["searchTerm"] = "UNIT old selected" },
		"old result ids":      func(t *Task) { t.Filters["resultIDs"] = "old" },
		"old followup":        func(t *Task) { t.Filters[PlaceFollowupIDFilter] = "old" },
		"private gps": func(t *Task) {
			t.Filters["currentQuery"] = "只看我家附近"
			t.Conversation[len(t.Conversation)-1].Text = "只看我家附近"
		},
		"viewport without bounds": func(t *Task) { t.Filters["locationPreference"] = "viewport" },
		"city with stale bounds": func(t *Task) {
			for k, v := range (&MapBounds{West: -3, South: 56, East: -1, North: 58}).Filters() {
				t.Filters[k] = v
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			task := emptyRefinementUnitTask()
			change(&task)
			if IsExplicitPlaceRefinementTask(task) {
				t.Fatal("invalid refinement accepted")
			}
		})
	}
}

func TestEmptyPlaceRefinementScopeKeepsCanonicalCategoryAndName(t *testing.T) {
	task := emptyRefinementUnitTask()
	task.Filters["currentQuery"] = "current map area instead"
	task.Conversation[len(task.Conversation)-1].Text = task.Filters["currentQuery"]
	task.Filters["locationPreference"] = "viewport"
	task.Filters["searchTerm"] = "UNIT explicit public name"
	for k, v := range (&MapBounds{West: -3, South: 56, East: -1, North: 58}).Filters() {
		task.Filters[k] = v
	}
	if !IsExplicitPlaceRefinementTask(task) {
		t.Fatal("closed map scope rejected")
	}
	if EmptyPlaceRefinementReply(task) == NativeRuleReply("place", 0) {
		t.Fatal("empty category/scope needs concrete truthful wording")
	}
}
