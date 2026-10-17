package agentworkspace

import (
	"strings"
	"testing"
)

func TestSelectedPlaceFollowupClosedFactsAndDayDraft(t *testing.T) {
	for _, tc := range []struct {
		query, topic string
		day          int
	}{
		{"它的地址是什么？", PlaceAddress, 0}, {"地址", PlaceAddress, 0},
		{"它周末几点开门？", PlaceOpeningHours, 0}, {"What are its opening hours?", PlaceOpeningHours, 0},
		{"把这个加入第二天", PlacePublicInformation, 2}, {"add this place to day 2", PlacePublicInformation, 2},
	} {
		f, day, ok := ParseSelectedPlaceFollowup(tc.query)
		if !ok || f.Topic != tc.topic || day != tc.day || !f.Pronoun || f.Name != "" {
			t.Fatalf("lost literal selected topic/day: %q %#v %d", tc.query, f, day)
		}
	}
	for _, query := range []string{"明天", "加入明天", "找餐厅", "Gallery的地址", "把这个加入第二天并发消息", "它的私人住址", "帮我预订它", "记住我喜欢这个地方"} {
		if _, _, ok := ParseSelectedPlaceFollowup(query); ok {
			t.Fatalf("unrelated/private/action entered selected local path: %q", query)
		}
	}
	text := SelectedPlaceDayDraftText("UNIT 原生地点")
	if !strings.Contains(text, "UNIT 原生地点") || !strings.Contains(text, "第2天") || !strings.Contains(text, "尚未保存") || !strings.Contains(text, "？") {
		t.Fatal("day draft lost truthful target clarification", text)
	}
}

func TestSelectedPlaceSelectorIsIdentityOnlyAndContextClears(t *testing.T) {
	for _, id := range []string{"", "osm:node:123", "00000000-0000-0000-0000-000000000000", "11111111-1111-4111-8111-111111111111?grant=1"} {
		if (SelectedPlaceSelector{PlaceID: id}).Valid() {
			t.Fatal("invalid native selector accepted")
		}
	}
	if !(SelectedPlaceSelector{PlaceID: "11111111-1111-4111-8111-111111111111"}).Valid() {
		t.Fatal("canonical native ID rejected")
	}
	task := Task{Filters: map[string]string{SelectedPlaceContextFilter: "native-map-v1", SelectedPlaceDayFilter: "2", PlaceFollowupIDFilter: "id", PlaceFollowupGenerationFilter: "proof", PlaceFollowupTopicFilter: PlacePublicInformation, "unrelated": "keep"}}
	ClearPlaceFollowupContext(&task)
	if len(task.Filters) != 1 || task.Filters["unrelated"] != "keep" {
		t.Fatal("selector/day survived retirement or unrelated task was erased")
	}
}
