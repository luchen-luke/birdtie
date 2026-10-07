package agentplanner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testID = "80000000-0000-4000-8000-000000000001"

func testView() View {
	n := time.Now().UTC().Truncate(time.Microsecond)
	v := NewView(Proposed, testID, testID, "readonly", n, n.Add(time.Second))
	v.Actions = []ActionProposal{Proposal(testID, 1, ActivitySearch, Arguments{Query: "羽毛球", CityID: "aberdeen-gb"}, strings.Repeat("a", 64), "审阅当前候选")}
	return v
}
func TestBoundedPlannerClosedPresentationSchema(t *testing.T) {
	raw, _ := json.Marshal(testView())
	if _, e := Decode(raw); e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct{ name, raw string }{
		{"confirmed", strings.Replace(string(raw), `"arguments":{`, `"confirmed":true,"arguments":{`, 1)},
		{"requires_confirmation", strings.Replace(string(raw), `"arguments":{`, `"requires_confirmation":false,"arguments":{`, 1)},
		{"permission", strings.Replace(string(raw), `"query":"羽毛球"`, `"query":"羽毛球","permission_override":"ALLOW"`, 1)},
		{"code", strings.Replace(string(raw), `"query":"羽毛球"`, `"query":"羽毛球","code":"rm -rf"`, 1)},
		{"unknown_tool", strings.Replace(string(raw), `activity.search`, `message.send`, 1)},
		{"alias", strings.Replace(string(raw), `"tool":`, `"Tool":`, 1)},
		{"duplicate", strings.Replace(string(raw), `"tool":`, `"tool":"shell","tool":`, 1)},
		{"bad_union", strings.Replace(string(raw), `"query":"羽毛球"`, `"query":"羽毛球","activity_id":"`+testID+`"`, 1)},
		{"null_actions", strings.Replace(string(raw), `"actions":[`, `"actions":null,"extra":[`, 1)},
		{"tail", string(raw) + ` {}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, e := Decode([]byte(c.raw)); e == nil {
				t.Fatal("authority/ambiguous schema accepted")
			}
		})
	}
}
func TestBoundedPlannerStepClockAndStableProposal(t *testing.T) {
	v := testView()
	a := v.Actions[0]
	if !a.Valid() || a.ActionID != Proposal(testID, 1, a.Tool, a.Arguments, a.ResourceVersion, a.ReasonSummary).ActionID {
		t.Fatal("unstable proposal")
	}
	b := a
	b.Arguments.Query = "篮球"
	if StableID(testID, 1, b.Tool, b.Arguments, b.ResourceVersion) == a.ActionID {
		t.Fatal("changed content retained ID")
	}
	for _, c := range []struct {
		name   string
		change func(*View)
	}{{"step_cap", func(v *View) {
		for len(v.Actions) <= MaxSteps {
			p := a
			p.ActionID = StableID(testID, len(v.Actions)+1, p.Tool, p.Arguments, p.ResourceVersion)
			v.Actions = append(v.Actions, p)
		}
	}}, {"clock_cap", func(v *View) { v.ValidUntil = v.ObservedAt.Add(MaxElapsed + time.Second) }}, {"expired", func(v *View) { v.ValidUntil = v.ObservedAt }}, {"two_questions", func(v *View) {
		v.Status = Clarification
		v.Actions = []ActionProposal{}
		v.Clarifications = []string{"一？", "二？", "三？"}
	}}, {"wrong_op", func(v *View) { v.Actions[0].LogicalOperationID = "80000000-0000-4000-8000-000000000002" }}} {
		t.Run(c.name, func(t *testing.T) {
			v := Clone(v)
			c.change(&v)
			if v.Valid(v.ObservedAt) {
				t.Fatal("unbounded/changed proposal accepted")
			}
		})
	}
}

func TestBoundedPlannerNativeRelativeClockCannotExtendLifetime(t *testing.T) {
	started := time.Now()
	v := testView()
	v.ObservedAt = started.Add(time.Hour).UTC()
	v.ValidUntil = v.ObservedAt.Add(time.Second)
	if v.Valid(started) || !v.ValidElapsed(started, started.Add(time.Millisecond)) {
		t.Fatal("absolute clock was borrowed as native currentness")
	}
	if v.ValidElapsed(started, started.Add(time.Second)) || v.ValidElapsed(started, started.Add(-time.Millisecond)) {
		t.Fatal("relative native bound extended or reversed")
	}
	v.ValidUntil = v.ObservedAt.Add(MaxElapsed + time.Second)
	if v.ValidElapsed(started, started) {
		t.Fatal("relative clock bypassed hard cap")
	}
}
