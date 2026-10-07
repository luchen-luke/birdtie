package agentseed

import (
	"encoding/json"
	"strings"
	"testing"
)

func goodInput() Input {
	return Input{ExpectedSnapshot: strings.Repeat("a", 64), Action: "SAVE", DisplayName: "中文姓名", CurrentCityID: "aberdeen-gb", CurrentCitySnapshot: strings.Repeat("b", 64), LanguagePreferences: []string{"zh-CN"}, BasicIntent: "JUST_EXPLORE", InterestChoice: "SKIP", Interests: []string{}}
}
func TestSeedWireAndExplicitChoices(t *testing.T) {
	v := goodInput()
	raw, _ := json.Marshal(v)
	got, e := Decode(raw)
	if e != nil || got.BasicIntent != "JUST_EXPLORE" {
		t.Fatal(got, e)
	}
	for _, intent := range []string{"FIND_PEOPLE", "FIND_ACTIVITIES", "EXPLORE_CITY", "SIMILAR_INTERESTS", "JOIN_COMMUNITIES", "DISCOVER_PLACES", "JUST_EXPLORE"} {
		t.Run(intent, func(t *testing.T) {
			v := goodInput()
			v.BasicIntent = intent
			if _, e := Normalize(v); e != nil {
				t.Fatal(e)
			}
		})
	}
	deferInput := Input{ExpectedSnapshot: strings.Repeat("a", 64), Action: "DEFER"}
	if _, e := Normalize(deferInput); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"ownerId", "accountId", "agentId", "verified", "confirmed", "purpose", "AgentNotes", "Action"} {
		t.Run(key, func(t *testing.T) {
			bad := strings.TrimSuffix(string(raw), "}") + `,"` + key + `":"fake"}`
			if _, e := Decode([]byte(bad)); e == nil {
				t.Fatal("accepted authority/unknown")
			}
		})
	}
	for _, bad := range []string{`null`, `{}`, `{"action":"DEFER","action":"SAVE"}`, `{"action":"DEFER","expectedSnapshot":null}`, strings.Replace(string(raw), `"JUST_EXPLORE"`, `"INFERRED"`, 1), strings.Replace(string(raw), `["zh-CN"]`, `[]`, 1), strings.Replace(string(raw), `"中文姓名"`, `"\ud800"`, 1), strings.TrimSuffix(string(raw), "}") + `,"interests":[]}`} {
		if _, e := Decode([]byte(bad)); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	v = goodInput()
	v.Action = "DEFER"
	if _, e := Normalize(v); e == nil {
		t.Fatal("defer cannot carry hidden edits")
	}
	v = goodInput()
	v.Interests = []string{"偷偷推断"}
	if _, e := Normalize(v); e == nil {
		t.Fatal("skip must not set interests")
	}
}
